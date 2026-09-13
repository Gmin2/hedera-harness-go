package tui

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/demo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
)

const agentPrompt = "add a scheduled payout from bob to carol and make sure the schedule executes"

func newAgentModel(w, h int) *Model {
	opts := testOptions()
	opts.AgentName = "claude"
	opts.Agent = func(context.Context, AgentRequest, event.Sink) error { return nil }
	m := newModel(context.Background(), opts)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// startAgent sends the test prompt through the editor and feeds the scripted
// turn up to and including the event where stop returns true, or all of it
// when stop is nil.
func startAgent(t *testing.T, m *Model, stop func(event.Event) bool) {
	t.Helper()
	sendPrompt(t, m, agentPrompt, stop)
}

func sendPrompt(t *testing.T, m *Model, prompt string, stop func(event.Event) bool) {
	t.Helper()
	sessionID := ""
	if m.session != nil {
		sessionID = m.session.SessionID()
	}
	typeText(m, prompt)
	press(m, "enter")
	if m.session == nil || m.session.Prompt != prompt {
		t.Fatal("prompt did not start an agent turn")
	}
	req := demo.Request{Prompt: prompt, Judges: m.judges, Network: m.network, SessionID: sessionID}
	for _, ev := range demo.AgentEvents(req, m.session.Turn()) {
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
	opts.Agent = func(ctx context.Context, req AgentRequest, sink event.Sink) error {
		defer close(finished)
		gotPrompt, gotJudges = req.Prompt, req.Judges
		for _, ev := range demo.AgentEvents(demo.Request(req), 1) {
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

// recordAgent swaps the agent func for one that only records requests, and
// returns the list it appends to.
func recordAgent(m *Model) *[]AgentRequest {
	var reqs []AgentRequest
	m.opts.Agent = func(_ context.Context, req AgentRequest, _ event.Sink) error {
		reqs = append(reqs, req)
		return nil
	}
	return &reqs
}

// converse submits prompt, runs the agent func and feeds the demo turn for
// the request it got.
func converse(t *testing.T, m *Model, reqs *[]AgentRequest, prompt string) {
	t.Helper()
	typeText(m, prompt)
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatalf("%q did not start a turn", prompt)
	}
	done := m.submitCmd(t, cmd)
	req := (*reqs)[len(*reqs)-1]
	for _, ev := range demo.AgentEvents(demo.Request(req), m.session.Turn()) {
		m.Update(eventMsg{gen: m.runGen, ev: ev})
	}
	m.Update(done)
}

// submitCmd runs the batch returned for a submit and hands back the message
// the agent func finished with.
func (m *Model) submitCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				walk(c)
			}
		case launchDoneMsg:
			msgs = append(msgs, msg)
		}
	}
	walk(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one agent call, got %d", len(msgs))
	}
	return msgs[0]
}

// listText is the whole conversation list without styling.
func listText(m *Model) string {
	var b strings.Builder
	for _, it := range m.session.Items() {
		b.WriteString(ansi.Strip(it.Render(200)))
		b.WriteString("\n")
	}
	return b.String()
}

func TestAgentConversation(t *testing.T) {
	m := newAgentModel(120, 32)
	reqs := recordAgent(m)

	converse(t, m, reqs, "add a payout helper")
	first := m.session
	if first.SessionID() != demo.SessionID {
		t.Fatalf("session id = %q", first.SessionID())
	}
	if m.editor.Placeholder != "Reply, or /new to start over" {
		t.Fatalf("placeholder = %q", m.editor.Placeholder)
	}

	converse(t, m, reqs, "now log every payout")
	if len(*reqs) != 2 || (*reqs)[0].SessionID != "" || (*reqs)[1].SessionID != demo.SessionID {
		t.Fatalf("requests = %+v", *reqs)
	}
	if m.session != first || m.session.Turn() != 2 {
		t.Fatal("a follow up prompt should continue the same session")
	}
	text := listText(m)
	for _, want := range []string{"add a payout helper", "The payout helper is in place", "now log every payout", "Picking up where we left off"} {
		if !strings.Contains(text, want) {
			t.Errorf("list is missing %q", want)
		}
	}
	if strings.Index(text, "add a payout helper") > strings.Index(text, "now log every payout") {
		t.Error("the follow up prompt should come after the first turn")
	}
	if strings.Contains(text, "Attempt 1") || strings.Contains(text, "done in") {
		t.Error("chat turns without judges should have no attempt rules or loop done line")
	}
	if got := m.session.Cost(); got < 0.139 || got > 0.141 {
		t.Fatalf("conversation cost = %.3f, want 0.14", got)
	}
	requireGolden(t, m)
}

func TestAgentNewConversation(t *testing.T) {
	for _, line := range []string{"/new", "new"} {
		t.Run(line, func(t *testing.T) {
			m := newAgentModel(120, 32)
			reqs := recordAgent(m)
			converse(t, m, reqs, "write the first helper")

			typeText(m, line)
			press(m, "enter")
			if m.session != nil || m.state != stateLanding {
				t.Fatalf("%s should drop the conversation", line)
			}
			converse(t, m, reqs, "start again")
			if got := (*reqs)[1].SessionID; got != "" {
				t.Fatalf("prompt after %s continued session %q", line, got)
			}
			if strings.Contains(listText(m), "write the first helper") {
				t.Fatal("the old turn is still in the list")
			}
		})
	}
}

func TestAgentCanceledKeepsSession(t *testing.T) {
	m := newAgentModel(120, 32)
	reqs := recordAgent(m)
	addJudges(m, "token-flow")
	startAgent(t, m, func(ev event.Event) bool {
		_, ok := ev.(event.AgentFinished)
		return ok
	})
	press(m, "esc")
	m.Update(launchDoneMsg{gen: m.runGen, err: context.Canceled})
	if m.running() || !m.session.Canceled() || m.session.SessionID() != demo.SessionID {
		t.Fatalf("canceled turn should keep the session, got %q", m.session.SessionID())
	}
	converse(t, m, reqs, "keep going")
	if (*reqs)[0].SessionID != demo.SessionID {
		t.Fatalf("prompt after esc sent session %q", (*reqs)[0].SessionID)
	}
}

func TestAgentStreaming(t *testing.T) {
	m := newAgentModel(120, 32)
	typeText(m, "say hello")
	press(m, "enter")
	feed := func(evs ...event.Event) {
		for _, ev := range evs {
			m.Update(eventMsg{gen: m.runGen, ev: ev})
		}
	}
	feed(event.AgentStarted{Attempt: 1, Turn: 1},
		event.AgentTextDelta{Attempt: 1, Text: "Hello "},
		event.AgentTextDelta{Attempt: 1, Text: "from the "},
		event.AgentTextDelta{Attempt: 1, Text: "stream"},
	)
	if text := listText(m); !strings.Contains(text, "Hello from the stream") {
		t.Fatalf("deltas did not render:\n%s", text)
	}
	if !strings.Contains(m.View().Content, "Hello from the stream") {
		t.Fatal("the live text is not on screen")
	}

	feed(event.AgentText{Attempt: 1, Text: "Hello from the stream."})
	if n := strings.Count(listText(m), "Hello from the stream"); n != 1 {
		t.Fatalf("final text shows %d times, want once", n)
	}

	feed(event.AgentTextDelta{Attempt: 1, Text: "Reading "},
		event.AgentTool{Attempt: 1, ID: "t1", Name: "Read", Summary: "go.mod"},
		event.AgentText{Attempt: 1, Text: "No deltas here."},
		event.AgentTextDelta{Attempt: 1, Text: "Second "},
		event.AgentText{Attempt: 1, Text: "Second block."},
	)
	text := listText(m)
	for _, want := range []string{"Reading", "No deltas here.", "Second block."} {
		if n := strings.Count(text, want); n != 1 {
			t.Errorf("%q shows %d times, want once:\n%s", want, n, text)
		}
	}
	if strings.Index(text, "Reading") > strings.Index(text, "Read go.mod") {
		t.Error("text written before a tool call should stay above it")
	}
}

func TestAgentBusy(t *testing.T) {
	m := newAgentModel(120, 32)
	startAgent(t, m, func(ev event.Event) bool {
		_, ok := ev.(event.AgentTool)
		return ok
	})
	gen := m.runGen
	typeText(m, "and another thing")
	press(m, "enter")
	if m.runGen != gen || m.session.Prompt != agentPrompt {
		t.Fatal("a prompt while running should not start a second turn")
	}
	if m.toast.kind != toastWarn || m.toast.text != "claude is still working, esc to cancel" {
		t.Fatalf("toast = %+v", m.toast)
	}
	if m.editor.Value() != "and another thing" {
		t.Fatalf("the refused prompt should stay in the editor, got %q", m.editor.Value())
	}
}

func TestSlashCommands(t *testing.T) {
	m := newAgentModel(120, 32)

	typeText(m, "/judge token-flow")
	press(m, "enter")
	if len(m.judges) != 1 || m.session != nil {
		t.Fatalf("/judge should add a judge, judges %v", m.judges)
	}

	typeText(m, "/network local")
	press(m, "enter")
	if m.network != "local" {
		t.Fatalf("/network did not switch, network %q", m.network)
	}

	typeText(m, "/run the payout scenario please")
	press(m, "enter")
	if m.toast.kind != toastError || m.session != nil || m.run != nil {
		t.Fatal("/run with an unknown scenario should never become a prompt")
	}

	typeText(m, "/deploy")
	press(m, "enter")
	if m.toast.kind != toastError || !strings.Contains(m.toast.text, "/deploy") || m.session != nil {
		t.Fatalf("unknown slash command toast = %+v", m.toast)
	}

	typeText(m, "/run token-flow")
	press(m, "enter")
	if m.run == nil || m.session != nil {
		t.Fatal("/run should start the scenario")
	}
	m.Update(launchDoneMsg{gen: m.runGen})

	typeText(m, "/clear")
	press(m, "enter")
	if m.run != nil || m.state != stateLanding {
		t.Fatal("/clear should drop the run")
	}

	press(m, "ctrl+p")
	typeText(m, "new conv")
	_, cmd := m.Update(keyMsg("enter"))
	if m.dialog.HasDialogs() || cmd == nil || m.toast.text != "new conversation" {
		t.Fatalf("the palette should start a new conversation, toast %+v", m.toast)
	}

	typeText(m, "/quit")
	_, cmd = m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("/quit returned nothing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/quit should quit")
	}
}
