package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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
	Prompt string
	Dir    string
	Judges []string // scenario files, relative to Dir or absolute
	Checks []Check  // shell commands that must exit 0, run before the scenarios
	// Env is added to the environment of checks.
	Env []string
	// CheckEnv, when set, prepares the environment for one round of checks,
	// eg a burner wallet funded for that round, and cleans it up after.
	CheckEnv    func(ctx context.Context) ([]string, func(), error)
	Network     network.Mode
	MaxAttempts int
	Model       string
	HH          string // path of the hh binary the agent can call
	Agent       Runner
	AgentName   string
	// SessionID continues an earlier conversation, Turn numbers this prompt in it.
	SessionID string
	Turn      int
	// MaxCostUSD stops the loop once the attempts together cost this much.
	MaxCostUSD float64
	// AttemptTimeout bounds one agent attempt, zero means no limit.
	AttemptTimeout time.Duration
	// Stream sends text as the agent writes it.
	Stream bool
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
	if o.Turn <= 0 {
		o.Turn = 1
	}
	start := time.Now()
	total := 0.0
	system := SystemPrompt(o.HH, o.Judges, o.Checks, string(o.Network))
	// judges that exist before the agent starts are the contract, it may not
	// rewrite them to pass. judges it is asked to create stay editable.
	pinned := pinJudges(o.Dir, o.Judges)
	prompt, session := o.Prompt, o.SessionID

	finish := func(status event.Status, attempts int, err error) error {
		lf := event.LoopFinished{Status: status, SessionID: session, Attempts: attempts, CostUSD: total, Elapsed: time.Since(start)}
		if err != nil {
			lf.Error = err.Error()
		}
		sink(lf)
		return err
	}

	for attempt := 1; attempt <= o.MaxAttempts; attempt++ {
		budget := 0.0
		if o.MaxCostUSD > 0 {
			budget = o.MaxCostUSD - total
			if budget < 0.01 {
				return finish(event.Failed, attempt-1, fmt.Errorf("budget of $%g used up", o.MaxCostUSD))
			}
		}
		sink(event.AgentStarted{
			Attempt:     attempt,
			MaxAttempts: o.MaxAttempts,
			Turn:        o.Turn,
			Agent:       o.AgentName,
			Model:       o.Model,
			Prompt:      prompt,
			Repair:      attempt > 1,
			Dir:         o.Dir,
		})
		attemptCtx, cancel := ctx, context.CancelFunc(func() {})
		if o.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, o.AttemptTimeout)
		}
		res, err := o.Agent.Run(attemptCtx, Request{
			Prompt:       prompt,
			Dir:          o.Dir,
			Model:        o.Model,
			SessionID:    session,
			SystemPrompt: system,
			Stream:       o.Stream,
			MaxBudgetUSD: budget,
		}, attempt, sink)
		timedOut := attemptCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil
		cancel()
		if timedOut {
			err = fmt.Errorf("attempt took longer than %s", o.AttemptTimeout)
		}
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
		if res.SessionID != "" {
			session = res.SessionID
		}
		sink(fin)
		if ctx.Err() != nil {
			return finish(event.Failed, attempt, ctx.Err())
		}
		if err != nil {
			return finish(event.Failed, attempt, err)
		}

		if len(o.Judges) == 0 && len(o.Checks) == 0 {
			return finish(event.Passed, attempt, nil)
		}
		findings := judge(ctx, o, attempt, pinned, sink)
		if ctx.Err() != nil {
			return finish(event.Failed, attempt, ctx.Err())
		}
		if len(findings) == 0 {
			return finish(event.Passed, attempt, nil)
		}
		prompt = RepairPrompt(findings, o.Judges, o.Checks, o.HH, string(o.Network))
	}
	return finish(event.Failed, o.MaxAttempts, fmt.Errorf("judge still failing after %d attempts", o.MaxAttempts))
}

// judge runs the checks, then every judge scenario, and returns what went
// wrong in a form the agent can act on. Checks go first: they are usually a
// build, and scenarios mean little when the code does not compile.
func judge(ctx context.Context, o Options, attempt int, pinned map[string]string, sink event.Sink) []string {
	var findings []string
	passed, failed := 0, 0
	env := o.Env
	checks := o.Checks
	if len(checks) > 0 && o.CheckEnv != nil {
		extra, cleanup, err := o.CheckEnv(ctx)
		if err != nil {
			// without the wallet the checks would fail for the wrong reason
			findings = append(findings, "could not prepare the wallet for checks: "+err.Error())
			failed++
			checks = nil
		} else {
			defer cleanup()
			env = append(append([]string(nil), env...), extra...)
		}
	}
	for _, c := range checks {
		if ctx.Err() != nil {
			break
		}
		fin := runCheck(ctx, o.Dir, c, attempt, env, sink)
		if fin.Status == event.Passed {
			passed++
		} else {
			failed++
			findings = append(findings, checkFinding(fin))
		}
	}
	for _, path := range o.Judges {
		if want, ok := pinned[path]; ok && fileHash(resolve(o.Dir, path)) != want {
			failed++
			findings = append(findings, fmt.Sprintf("%s: this judge scenario was changed during the run. restore it with git and fix the code instead, judges are not yours to edit", path))
			continue
		}
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

func pinJudges(dir string, judges []string) map[string]string {
	out := map[string]string{}
	for _, j := range judges {
		if h := fileHash(resolve(dir, j)); h != "" {
			out[j] = h
		}
	}
	return out
}

func fileHash(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func resolve(dir, path string) string {
	if dir == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
