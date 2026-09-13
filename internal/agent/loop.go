package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/runner"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

// Runner is anything that can do one agent turn. Claude is the real one,
// tests use a fake.
type Runner interface {
	Run(ctx context.Context, req Request, attempt int, sink event.Sink) (Result, error)
}

type Options struct {
	Prompt      string
	Dir         string
	Judges      []string // scenario files, relative to Dir or absolute
	Network     network.Mode
	MaxAttempts int
	Model       string
	HH          string // path of the hh binary the agent can call
	Agent       Runner
	AgentName   string
	// Open connects to the judge network. The cli passes the same helper
	// hh run uses, so a judge run is identical to a manual one.
	Open func(ctx context.Context, mode network.Mode) (*network.Target, func(), error)
}

// Loop runs the agent, judges the result, and sends repair prompts until the
// judge passes or attempts run out.
func Loop(ctx context.Context, o Options, sink event.Sink) error {
	if sink == nil {
		sink = func(event.Event) {}
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.AgentName == "" {
		o.AgentName = "claude"
	}
	start := time.Now()
	total := 0.0
	system := SystemPrompt(o.HH, o.Judges, string(o.Network))

	finish := func(status event.Status, attempts int, err error) error {
		lf := event.LoopFinished{Status: status, Attempts: attempts, CostUSD: total, Elapsed: time.Since(start)}
		if err != nil {
			lf.Error = err.Error()
		}
		sink(lf)
		return err
	}

	prompt, session := o.Prompt, ""
	for attempt := 1; attempt <= o.MaxAttempts; attempt++ {
		sink(event.AgentStarted{
			Attempt: attempt,
			Agent:   o.AgentName,
			Model:   o.Model,
			Prompt:  prompt,
			Repair:  attempt > 1,
			Dir:     o.Dir,
		})
		res, err := o.Agent.Run(ctx, Request{
			Prompt:       prompt,
			Dir:          o.Dir,
			Model:        o.Model,
			SessionID:    session,
			SystemPrompt: system,
		}, attempt, sink)
		total += res.CostUSD
		fin := event.AgentFinished{
			Attempt:   attempt,
			Status:    event.Passed,
			SessionID: res.SessionID,
			Model:     res.Model,
			Turns:     res.Turns,
			CostUSD:   res.CostUSD,
			Result:    res.Text,
			Elapsed:   res.Elapsed,
		}
		if err != nil {
			fin.Status, fin.Error = event.Failed, err.Error()
		}
		sink(fin)
		if ctx.Err() != nil {
			return finish(event.Failed, attempt, ctx.Err())
		}
		if err != nil {
			return finish(event.Failed, attempt, err)
		}
		if res.SessionID != "" {
			session = res.SessionID
		}

		if len(o.Judges) == 0 {
			return finish(event.Passed, attempt, nil)
		}
		findings := judge(ctx, o, attempt, sink)
		if ctx.Err() != nil {
			return finish(event.Failed, attempt, ctx.Err())
		}
		if len(findings) == 0 {
			return finish(event.Passed, attempt, nil)
		}
		prompt = RepairPrompt(findings, o.Judges, o.HH, string(o.Network))
	}
	return finish(event.Failed, o.MaxAttempts, fmt.Errorf("judge still failing after %d attempts", o.MaxAttempts))
}

// judge runs every judge scenario and returns what went wrong, one finding
// per line, in a form the agent can act on.
func judge(ctx context.Context, o Options, attempt int, sink event.Sink) []string {
	var findings []string
	passed, failed := 0, 0
	for _, path := range o.Judges {
		fs := judgeOne(ctx, o, path, sink)
		if len(fs) == 0 {
			passed++
		} else {
			failed++
			findings = append(findings, fs...)
		}
	}
	jf := event.JudgeFinished{Attempt: attempt, Status: event.Passed, Passed: passed, Failed: failed, Findings: findings}
	if failed > 0 {
		jf.Status = event.Failed
	}
	sink(jf)
	return findings
}

func judgeOne(ctx context.Context, o Options, path string, sink event.Sink) []string {
	// reload every attempt, the agent may have written or fixed the file
	sc, err := scenario.Load(resolve(o.Dir, path))
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", path, err)}
	}
	if err := runner.Plan(sc); err != nil {
		var out []string
		for _, line := range strings.Split(err.Error(), "\n") {
			out = append(out, fmt.Sprintf("%s: %s", path, line))
		}
		return out
	}
	t, closeTarget, err := o.Open(ctx, o.Network)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot reach the %s network: %v", path, o.Network, err)}
	}
	defer closeTarget()

	rep := runner.Run(ctx, t, sc, runner.Defaults(o.Network), sink)
	return Findings(path, rep)
}

// Findings turns a failed report into short lines for a repair prompt.
func Findings(path string, rep *runner.Report) []string {
	var out []string
	for _, s := range rep.Steps {
		if s.Status != event.Failed {
			continue
		}
		msg := s.Error
		if msg == "" {
			msg = fmt.Sprintf("expected %s, got %s", s.Expected, s.Receipt)
		}
		out = append(out, fmt.Sprintf("%s: step %d %s failed: %s", path, s.Index+1, s.Op, msg))
	}
	for _, a := range rep.Asserts {
		if a.Status != event.Failed {
			continue
		}
		line := fmt.Sprintf("%s: assertion %s (%s) expected %s, actual %s", path, a.Kind, a.Title, a.Expected, a.Actual)
		if a.Error != "" {
			line += ": " + a.Error
		}
		out = append(out, line)
	}
	if len(out) == 0 && rep.Status == event.Failed {
		msg := rep.Error
		if msg == "" {
			msg = "run failed"
		}
		out = append(out, fmt.Sprintf("%s: %s", path, msg))
	}
	return out
}

func resolve(dir, path string) string {
	if dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
