package tui

import (
	"context"
	"io"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/demo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
)

const agentPrompt = "add a scheduled payout from bob to carol and make sure the schedule executes"

func newAgentModel(w, h int) *Model {
	opts := testOptions()
	opts.AgentName = "claude"
	opts.Agent = func(context.Context, string, []string, string, event.Sink) error { return nil }
	m := newModel(context.Background(), opts)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// startAgent sends prompt through the editor and feeds the scripted loop up
// to and including the event where stop returns true, or all of it when stop
// is nil.
func startAgent(t *testing.T, m *Model, stop func(event.Event) bool) {
	t.Helper()
	typeText(m, agentPrompt)
	press(m, "enter")
	if m.session == nil {
		t.Fatal("prompt did not start an agent session")
	}
	events := demo.AgentEvents(agentPrompt, m.judges, m.network)
	for _, ev := range events {
		m.Update(eventMsg{gen: m.runGen, ev: ev})
		if stop != nil && stop(ev) {
			return
		}
	}
	m.Update(launchDoneMsg{gen: m.runGen})
}

func addJudges(m *Model, names ...string) {
	for _, n := range names {
		typeText(m, "judge "+n)
		press(m, "enter")
	}
	m.Update(clearToastMsg{id: m.toast.id})
}

func TestLandingJudges(t *testing.T) {
	m := newAgentModel(120, 32)
	addJudges(m, "token-flow", "scheduled-payout")
	if len(m.judges) != 2 {
		t.Fatalf("judges = %v", m.judges)
	}
	requireGolden(t, m)
}

func TestAgentMidTool(t *testing.T) {
	m := newAgentModel(120, 32)
	addJudges(m, "token-flow")
	startAgent(t, m, func(ev event.Event) bool {
		tool, ok := ev.(event.AgentTool)
		return ok && tool.Name == "Bash"
	})
	if !m.running() {
		t.Fatal("session should still be running")
	}
	requireGolden(t, m)
}

func TestAgentFinished(t *testing.T) {
	m := newAgentModel(120, 32)
	addJudges(m, "token-flow")
	startAgent(t, m, nil)

	s := m.session
	if s.Result() == nil || s.Result().Status != event.Passed {
		t.Fatalf("loop result = %+v", s.Result())
	}
	if len(s.Runs()) != 2 {
		t.Fatalf("judge runs = %d, want one per attempt", len(s.Runs()))
	}
	first, second := s.Runs()[0], s.Runs()[1]
	if first.Result() == nil || first.Result().Status != event.Failed {
		t.Fatal("the first judge run should have failed")
	}
	if len(s.Items()) < len(first.Items())+len(second.Items()) {
		t.Fatal("the first judge run was dropped from the list")
	}
	if j := s.Judges(); len(j) != 1 || j[0].Status != event.Passed {
		t.Fatalf("judges = %+v", j)
	}
	requireGolden(t, m)
}

func TestAgentRouting(t *testing.T) {
	m := newAgentModel(120, 32)

	typeText(m, "judge nope")
	press(m, "enter")
	if m.toast.kind != toastError || len(m.judges) != 0 {
		t.Fatal("an unknown judge should be refused")
	}

	addJudges(m, "token-flow")
	typeText(m, "judge off")
	press(m, "enter")
	if len(m.judges) != 0 {
		t.Fatal("judge off did not clear the judges")
	}

	m.opts.Scenarios = append(m.opts.Scenarios, Scenario{Name: "first transfer", Path: "scenarios/first-transfer.yaml"})
	typeText(m, "judge First Transfer")
	press(m, "enter")
	if len(m.judges) != 1 || m.session != nil {
		t.Fatal("a scenario name with spaces should be a judge command, not a prompt")
	}
	typeText(m, "judge off")
	press(m, "enter")

	typeText(m, "run token-flow")
	press(m, "enter")
	if m.run == nil || m.session != nil {
		t.Fatal("run with one argument should still run the scenario")
	}
	m.Update(launchDoneMsg{gen: m.runGen})

	typeText(m, "run the payout scenario and fix what fails")
	press(m, "enter")
	if m.session == nil || m.run != nil {
		t.Fatal("a sentence should go to the agent even when it starts with run")
	}
	if m.session.Prompt != "run the payout scenario and fix what fails" {
		t.Fatalf("prompt = %q", m.session.Prompt)
	}

	press(m, "esc")
	m.Update(launchDoneMsg{gen: m.runGen, err: context.Canceled})
	if m.running() || !m.session.Canceled() {
		t.Fatal("esc should cancel the agent loop")
	}

	typeText(m, "clear")
	press(m, "enter")
	if m.state != stateLanding || m.session != nil {
		t.Fatal("clear should drop the session")
	}
}

func TestJudgePicker(t *testing.T) {
	m := newAgentModel(120, 32)
	press(m, "ctrl+p")
	typeText(m, "judge")
	press(m, "enter")
	if !m.dialog.Contains(scenariosID) {
		t.Fatal("the palette entry should open the scenario picker")
	}
	press(m, "enter")
	if m.dialog.HasDialogs() || len(m.judges) != 1 || m.judges[0] != scenarioPath {
		t.Fatalf("picking a scenario should add it as a judge, judges = %v", m.judges)
	}
	if m.dialog.Contains(dialog.TestnetID) {
		t.Fatal("adding a judge should not ask about testnet")
	}
}

func TestAgentLayoutSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {90, 28}, {120, 32}, {200, 60}} {
		m := newAgentModel(size[0], size[1])
		addJudges(m, "token-flow", "topic-submit")
		checkBounds(t, m, m.View().Content)
		startAgent(t, m, nil)
		checkBounds(t, m, m.View().Content)
		if m.compact {
			press(m, "ctrl+d")
			checkBounds(t, m, m.View().Content)
		}
	}
}

// TestAgentProgram drives a headless program: type a prompt, let the agent
// func stream the demo loop through program.Send, then quit.
func TestAgentProgram(t *testing.T) {
	finished := make(chan struct{})
	opts := testOptions()
	opts.NoAnim = false
	opts.AgentName = "claude"
	var gotPrompt string
	var gotJudges []string
	opts.Agent = func(ctx context.Context, prompt string, judges []string, network string, sink event.Sink) error {
		defer close(finished)
		gotPrompt, gotJudges = prompt, judges
		for _, ev := range demo.AgentEvents(prompt, judges, network) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			sink(ev)
		}
		return nil
	}

	m := newModel(context.Background(), opts)
	p := tea.NewProgram(m,
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithWindowSize(120, 32),
		tea.WithoutSignals(),
	)
	m.send = p.Send

	go func() {
		for _, line := range []string{"judge topic-submit", "make the payout work"} {
			for _, r := range line {
				p.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		select {
		case <-finished:
			time.Sleep(100 * time.Millisecond)
		case <-time.After(5 * time.Second):
			t.Error("agent never finished")
		}
		p.Quit()
	}()

	final, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	fm := final.(*Model)
	if gotPrompt != "make the payout work" || !slices.Equal(gotJudges, []string{"scenarios/topic-submit.yaml"}) {
		t.Fatalf("agent got prompt %q judges %v", gotPrompt, gotJudges)
	}
	if fm.session == nil || fm.session.Result() == nil || fm.session.Result().Status != event.Passed {
		t.Fatal("the loop did not finish inside the program")
	}
}
