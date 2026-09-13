package demo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

const (
	agentName  = "claude"
	agentModel = "claude-sonnet-4-5"
)

// SessionID is the fake agent session every demo conversation reports.
const SessionID = "demo-session-7f3a2c"

// Request mirrors tui.AgentRequest. The tui tests import this package, so it
// cannot import the tui back.
type Request struct {
	Prompt    string
	Judges    []string
	Network   string
	SessionID string
}

// turns counts prompts in the current demo conversation, like the real agent
// func does. A request without a session starts over.
var turns struct {
	sync.Mutex
	n int
}

func nextTurn(sessionID string) int {
	turns.Lock()
	defer turns.Unlock()
	if sessionID == "" {
		turns.n = 0
	}
	turns.n++
	return turns.n
}

// Agent plays a scripted turn of a conversation into sink. On the first
// prompt with judges, attempt one leaves the scheduled payout unsigned, the
// judge catches it and attempt two fixes it. Follow up prompts continue the
// same session.
func Agent(ctx context.Context, req Request, sink event.Sink) error {
	for _, b := range AgentScript(req, nextTurn(req.SessionID)) {
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
func AgentEvents(req Request, turn int) []event.Event {
	beats := AgentScript(req, turn)
	out := make([]event.Event, len(beats))
	for i, b := range beats {
		out[i] = b.Event
	}
	return out
}

// AgentScript builds the fake turn. turn is the prompt number in the
// conversation, starting at 1. Text is streamed as deltas before each
// complete block.
func AgentScript(req Request, turn int) []Beat {
	judges, network := req.Judges, req.Network

	var beats []Beat
	add := func(d time.Duration, ev event.Event) {
		beats = append(beats, Beat{Delay: d, Event: ev})
	}
	say := func(attempt int, d time.Duration, text string) {
		words := strings.SplitAfter(text, " ")
		for i := 0; i < len(words); i += 3 {
			add(d, event.AgentTextDelta{Attempt: attempt, Text: strings.Join(words[i:min(i+3, len(words))], "")})
			d = 60 * time.Millisecond
		}
		add(0, event.AgentText{Attempt: attempt, Text: text})
	}
	tool := func(attempt int, id, name, summary, output string, took time.Duration) {
		add(300*time.Millisecond, event.AgentTool{Attempt: attempt, ID: id, Name: name, Summary: summary})
		add(took, event.AgentToolResult{Attempt: attempt, ID: id, Name: name, Output: output})
	}
	start := func(attempt int, prompt string) {
		add(400*time.Millisecond, event.AgentStarted{
			Attempt: attempt, MaxAttempts: 3, Turn: turn, Agent: agentName, Model: agentModel,
			Prompt: prompt, Repair: attempt > 1, Dir: ".",
		})
	}
	finish := func(attempt, claudeTurns int, cost float64, took time.Duration) {
		add(400*time.Millisecond, event.AgentFinished{
			Attempt: attempt, Status: event.Passed, SessionID: SessionID, Model: agentModel,
			Turns: claudeTurns, CostUSD: cost, Elapsed: took,
		})
	}
	judge := func(attempt int, broken bool) {
		passed, failed := 0, 0
		var findings []string
		for i, path := range judges {
			bad := broken && i == 0
			// judge runs play at double speed, the demo is about the loop
			for _, b := range script(path, network, runID(attempt, i), bad) {
				add(b.Delay/2, b.Event)
			}
			if bad {
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
	done := func(attempts int, cost float64, took time.Duration) {
		add(300*time.Millisecond, event.LoopFinished{
			Status: event.Passed, SessionID: SessionID, Attempts: attempts, CostUSD: cost, Elapsed: took,
		})
	}

	if turn > 1 {
		start(1, req.Prompt)
		say(1, time.Second, fmt.Sprintf("Picking up where we left off after turn %d. "+
			"The payout helper from earlier is still in place, so I will build on it for: %s",
			turn-1, strings.TrimSpace(req.Prompt)))
		tool(1, fmt.Sprintf("toolu_%d1", turn), "Read", "internal/payout/payout.go", payoutGo, 400*time.Millisecond)
		say(1, 900*time.Millisecond, "Nothing from the earlier turn needs redoing. The change is small and sits next to the scheduled payout.")
		finish(1, 3, 0.02, 9*time.Second)
		if len(judges) == 0 {
			done(1, 0.02, 9*time.Second)
			return beats
		}
		judge(1, false)
		done(1, 0.02, 21*time.Second)
		return beats
	}

	start(1, req.Prompt)
	say(1, 1200*time.Millisecond, "I'll read the payout scenario first so I know what the judge is going to check.")
	tool(1, "toolu_01", "Read", "scenarios/scheduled-payout.yaml", payoutYAML, 500*time.Millisecond)
	say(1, 1500*time.Millisecond, "The scenario schedules an hbar transfer from bob to carol and expects it to execute. "+
		"I'll add a payout helper that creates the schedule and wire it into the treasury command.")
	tool(1, "toolu_02", "Write", "internal/payout/payout.go", "File created successfully at: internal/payout/payout.go", 700*time.Millisecond)
	tool(1, "toolu_03", "Bash", "go test ./internal/payout/...", "ok  \tgithub.com/acme/treasury/internal/payout\t0.412s", 1800*time.Millisecond)
	say(1, 900*time.Millisecond, "The payout helper is in place and its unit tests pass.")
	finish(1, 7, 0.12, 41*time.Second)
	if len(judges) == 0 {
		done(1, 0.12, 41*time.Second)
		return beats
	}
	judge(1, true)

	start(2, "The judge scenarios failed. Fix the code so they pass.")
	say(2, 1500*time.Millisecond, "The schedule gets created but bob never signs it, so the mirror node never sees it execute. "+
		"I'll add bob's key to the schedule signers.")
	tool(2, "toolu_04", "Edit", "internal/payout/payout.go", "The file internal/payout/payout.go has been updated.", 600*time.Millisecond)
	tool(2, "toolu_05", "Bash", "hh run token-flow --network "+network, hhRunOutput(network), 2500*time.Millisecond)
	say(2, 900*time.Millisecond, "The scheduled payout now executes and the scenario passes locally.")
	finish(2, 4, 0.07, 23*time.Second)
	judge(2, false)

	done(2, 0.19, 2*time.Minute+10*time.Second)
	return beats
}

func runID(attempt, judge int) string {
	return fmt.Sprintf("8b%d%d1e", attempt, judge)
}

const payoutGo = `package payout

// Schedule creates a scheduled hbar transfer that bob signs.
func Schedule(c *hiero.Client, from, to hiero.AccountID, amount hiero.Hbar) (hiero.ScheduleID, error) {
	tx := hiero.NewTransferTransaction().
		AddHbarTransfer(from, amount.Negated()).
		AddHbarTransfer(to, amount)
	return schedule(c, tx, bobKey)
}
`

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
