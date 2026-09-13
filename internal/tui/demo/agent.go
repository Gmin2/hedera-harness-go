package demo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

const (
	agentName  = "claude"
	agentModel = "claude-sonnet-4-5"
)

// DefaultJudge is what the fake loop judges with when none are selected.
const DefaultJudge = "scenarios/token-flow.yaml"

// Agent plays a scripted generate and repair loop into sink. The first
// attempt leaves the scheduled payout unsigned, the judge catches it and the
// second attempt fixes it.
func Agent(ctx context.Context, prompt string, judges []string, network string, sink event.Sink) error {
	for _, b := range AgentScript(prompt, judges, network) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.Delay):
		}
		sink(b.Event)
	}
	return nil
}

// AgentEvents is the agent script without the pauses, for tests.
func AgentEvents(prompt string, judges []string, network string) []event.Event {
	beats := AgentScript(prompt, judges, network)
	out := make([]event.Event, len(beats))
	for i, b := range beats {
		out[i] = b.Event
	}
	return out
}

// AgentScript builds the fake loop: two attempts, each followed by a run of
// every judge. Only the first judge fails, and only on the first attempt.
func AgentScript(prompt string, judges []string, network string) []Beat {
	if len(judges) == 0 {
		judges = []string{DefaultJudge}
	}

	var beats []Beat
	add := func(d time.Duration, ev event.Event) {
		beats = append(beats, Beat{Delay: d, Event: ev})
	}
	tool := func(attempt int, id, name, summary, output string, took time.Duration) {
		add(300*time.Millisecond, event.AgentTool{Attempt: attempt, ID: id, Name: name, Summary: summary})
		add(took, event.AgentToolResult{Attempt: attempt, ID: id, Name: name, Output: output})
	}
	judge := func(attempt int) {
		passed, failed := 0, 0
		var findings []string
		for i, path := range judges {
			broken := attempt == 1 && i == 0
			// judge runs play at double speed, the demo is about the loop
			for _, b := range script(path, network, runID(attempt, i), broken) {
				add(b.Delay/2, b.Event)
			}
			if broken {
				failed++
				name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				findings = append(findings,
					name+": step schedule.create payout got INVALID_SIGNATURE, expected SUCCESS",
					name+": schedule.executed payout expected executed, actual not found",
				)
			} else {
				passed++
			}
		}
		status := event.Passed
		if failed > 0 {
			status = event.Failed
		}
		add(300*time.Millisecond, event.JudgeFinished{
			Attempt: attempt, Status: status, Passed: passed, Failed: failed, Findings: findings,
		})
	}

	add(400*time.Millisecond, event.AgentStarted{Attempt: 1, Agent: agentName, Model: agentModel, Prompt: prompt, Dir: "."})
	add(1200*time.Millisecond, event.AgentText{Attempt: 1, Text: "I'll read the payout scenario first so I know what the judge is going to check."})
	tool(1, "toolu_01", "Read", "scenarios/scheduled-payout.yaml", payoutYAML, 500*time.Millisecond)
	add(1500*time.Millisecond, event.AgentText{Attempt: 1, Text: "The scenario schedules an hbar transfer from bob to carol and expects it to execute. " +
		"I'll add a payout helper that creates the schedule and wire it into the treasury command."})
	tool(1, "toolu_02", "Write", "internal/payout/payout.go", "File created successfully at: internal/payout/payout.go", 700*time.Millisecond)
	tool(1, "toolu_03", "Bash", "go test ./internal/payout/...", "ok  \tgithub.com/acme/treasury/internal/payout\t0.412s", 1800*time.Millisecond)
	add(900*time.Millisecond, event.AgentText{Attempt: 1, Text: "The payout helper is in place and its unit tests pass."})
	add(400*time.Millisecond, event.AgentFinished{
		Attempt: 1, Status: event.Passed, SessionID: "demo-session", Model: agentModel,
		Turns: 7, CostUSD: 0.12, Elapsed: 41 * time.Second,
	})
	judge(1)

	add(600*time.Millisecond, event.AgentStarted{
		Attempt: 2, Agent: agentName, Model: agentModel, Repair: true, Dir: ".",
		Prompt: "The judge scenarios failed. Fix the code so they pass.",
	})
	add(1500*time.Millisecond, event.AgentText{Attempt: 2, Text: "The schedule gets created but bob never signs it, so the mirror node never sees it execute. " +
		"I'll add bob's key to the schedule signers."})
	tool(2, "toolu_04", "Edit", "internal/payout/payout.go", "The file internal/payout/payout.go has been updated.", 600*time.Millisecond)
	tool(2, "toolu_05", "Bash", "hh run token-flow --network "+network, hhRunOutput(network), 2500*time.Millisecond)
	add(900*time.Millisecond, event.AgentText{Attempt: 2, Text: "The scheduled payout now executes and the scenario passes locally."})
	add(400*time.Millisecond, event.AgentFinished{
		Attempt: 2, Status: event.Passed, SessionID: "demo-session", Model: agentModel,
		Turns: 4, CostUSD: 0.07, Elapsed: 23 * time.Second,
	})
	judge(2)

	add(300*time.Millisecond, event.LoopFinished{
		Status: event.Passed, Attempts: 2, CostUSD: 0.19, Elapsed: 2*time.Minute + 10*time.Second,
	})
	return beats
}

func runID(attempt, judge int) string {
	return fmt.Sprintf("8b%d%d1e", attempt, judge)
}

const payoutYAML = `name: scheduled-payout
network: mock
actors:
  bob: { hbar: 50 }
  carol: { hbar: 10 }
steps:
  - op: schedule.create
    target: payout
    params: { payer: bob, to: carol, amount: 1 }
assertions:
  - kind: schedule.executed
    title: payout
    expected: executed
`

func hhRunOutput(network string) string {
	return strings.Join([]string{
		"run token-flow on " + network,
		"actors   alice 0.0.1001  bob 0.0.1002  carol 0.0.1003",
		"✓ token.create gold          900ms",
		"✓ token.associate bob        450ms",
		"✓ token.transfer gold        520ms",
		"✓ token.transfer gold        480ms",
		"✓ hbar.transfer              400ms",
		"✓ topic.create news          610ms",
		"✓ topic.submit news          380ms",
		"✓ schedule.create payout     700ms",
		"6/6 assertions passed in 10.9s",
	}, "\n")
}
