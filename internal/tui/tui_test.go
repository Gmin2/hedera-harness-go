package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/demo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
)

const scenarioPath = "scenarios/token-flow.yaml"

func testOptions() Options {
	return Options{
		Version:  "v0.1.0",
		Cwd:      "/work/hh-demo",
		Network:  "mock",
		Networks: []string{"mock", "local", "testnet"},
		Operator: "0.0.2",
		NoAnim:   true,
		Scenarios: []Scenario{
			{Name: "token-flow", Path: scenarioPath, Steps: 8, Assertions: 6},
			{Name: "topic-submit", Path: "scenarios/topic-submit.yaml", Steps: 4, Assertions: 3},
			{Name: "scheduled-payout", Path: "scenarios/scheduled-payout.yaml", Steps: 6, Assertions: 4},
		},
		Launch: func(context.Context, string, string, event.Sink) error { return nil },
	}
}

func newTestModel(w, h int) *Model {
	m := newModel(context.Background(), testOptions())
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// startRun launches the scenario without running the launcher and feeds
// the first n scripted events, or all of them when n < 0.
func startRun(t *testing.T, m *Model, network string, n int) {
	t.Helper()
	m.network = network
	if cmd := m.requestRun(scenarioPath, true); cmd == nil {
		t.Fatal("run did not start")
	}
	m.relayout()
	events := demo.Events(scenarioPath, network)
	if n < 0 || n > len(events) {
		n = len(events)
	}
	for _, ev := range events[:n] {
		m.Update(eventMsg{gen: m.runGen, ev: ev})
	}
	if n == len(events) {
		m.Update(launchDoneMsg{gen: m.runGen})
	}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// requireGolden checks the frame twice: stripped of styling so layout
// changes read well in a diff, and raw so style regressions are caught too.
func requireGolden(t *testing.T, m *Model) {
	t.Helper()
	out := m.View().Content
	checkBounds(t, m, out)
	t.Run("plain", func(t *testing.T) { golden.RequireEqual(t, ansi.Strip(out)) })
	t.Run("ansi", func(t *testing.T) { golden.RequireEqual(t, out) })
}

func checkBounds(t *testing.T, m *Model, out string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > m.height {
		t.Errorf("frame has %d lines, terminal has %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d wide, terminal is %d: %q", i, w, m.width, ansi.Strip(l))
		}
	}
}

func TestLanding(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {90, 28}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			requireGolden(t, newTestModel(size[0], size[1]))
		})
	}
}

func TestLandingLastRun(t *testing.T) {
	m := newTestModel(120, 32)
	startRun(t, m, "mock", -1)
	typeText(m, "clear")
	press(m, "enter")
	if m.state != stateLanding {
		t.Fatal("clear did not return to the landing page")
	}
	requireGolden(t, m)
}

func TestRunStarting(t *testing.T) {
	m := newTestModel(120, 32)
	startRun(t, m, "mock", 0)
	requireGolden(t, m)
}

func TestRunMid(t *testing.T) {
	m := newTestModel(120, 32)
	// Up to the fourth step starting: actors ready, three steps done, one
	// in flight.
	startRun(t, m, "testnet", 11)
	requireGolden(t, m)
}

func TestRunFinished(t *testing.T) {
	m := newTestModel(120, 32)
	startRun(t, m, "testnet", -1)
	requireGolden(t, m)
}

func TestRunScrolled(t *testing.T) {
	m := newTestModel(120, 32)
	startRun(t, m, "testnet", -1)
	press(m, "tab", "g")
	if m.view.Following() {
		t.Fatal("still following after scrolling to the top")
	}
	requireGolden(t, m)
}

func TestCompact(t *testing.T) {
	m := newTestModel(90, 28)
	startRun(t, m, "mock", 20)
	if !m.compact {
		t.Fatal("90x28 should be compact")
	}
	t.Run("run", func(t *testing.T) { requireGolden(t, m) })

	press(m, "ctrl+d")
	if !m.detailsOpen {
		t.Fatal("ctrl+d did not open details")
	}
	t.Run("details", func(t *testing.T) { requireGolden(t, m) })
}

func TestDialogs(t *testing.T) {
	t.Run("commands", func(t *testing.T) {
		m := newTestModel(120, 32)
		press(m, "ctrl+p")
		typeText(m, "net")
		requireGolden(t, m)
	})
	t.Run("scenarios", func(t *testing.T) {
		m := newTestModel(90, 28)
		press(m, "ctrl+r")
		requireGolden(t, m)
	})
	t.Run("network", func(t *testing.T) {
		m := newTestModel(120, 32)
		press(m, "ctrl+n")
		requireGolden(t, m)
	})
	t.Run("testnet", func(t *testing.T) {
		m := newTestModel(120, 32)
		m.network = "testnet"
		typeText(m, "run token-flow")
		press(m, "enter")
		if !m.dialog.Contains(dialog.TestnetID) {
			t.Fatal("running on testnet did not ask first")
		}
		requireGolden(t, m)
	})
	t.Run("quit", func(t *testing.T) {
		m := newTestModel(120, 32)
		startRun(t, m, "mock", 8)
		press(m, "ctrl+c")
		requireGolden(t, m)
	})
}

func TestCommands(t *testing.T) {
	m := newTestModel(120, 32)

	typeText(m, "bogus")
	press(m, "enter")
	if m.toast.kind != toastError {
		t.Fatalf("unknown command should show an error toast, got %+v", m.toast)
	}

	typeText(m, "network testnet")
	press(m, "enter")
	if m.network != "testnet" || m.toast.kind != toastSuccess {
		t.Fatalf("network command did not switch: %q %+v", m.network, m.toast)
	}

	typeText(m, "run nope")
	press(m, "enter")
	if m.toast.kind != toastError || m.run != nil {
		t.Fatal("unknown scenario should not start a run")
	}

	typeText(m, "run token-flow")
	press(m, "enter")
	press(m, "y")
	if m.run == nil || !m.running() {
		t.Fatal("confirming the testnet dialog should start the run")
	}

	press(m, "esc")
	if m.cancel == nil {
		t.Fatal("cancel func missing")
	}
	m.Update(launchDoneMsg{gen: m.runGen, err: context.Canceled})
	if m.running() || !m.run.Canceled() {
		t.Fatal("run should be canceled")
	}

	press(m, "ctrl+c", "ctrl+c")
}

func TestToast(t *testing.T) {
	m := newTestModel(90, 28)
	typeText(m, "deploy everything")
	press(m, "enter")
	requireGolden(t, m)

	m.Update(clearToastMsg{id: m.toast.id})
	if m.toast.kind != toastNone {
		t.Fatal("toast did not clear")
	}
}

func TestQuitTwice(t *testing.T) {
	m := newTestModel(120, 32)
	m.Update(keyMsg("ctrl+c"))
	_, cmd := m.Update(keyMsg("ctrl+c"))
	if cmd == nil {
		t.Fatal("second ctrl+c should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("second ctrl+c should return tea.Quit")
	}
}

func TestStaleEventsIgnored(t *testing.T) {
	m := newTestModel(120, 32)
	startRun(t, m, "mock", 3)
	old := m.runGen
	m.Update(launchDoneMsg{gen: old, err: context.Canceled})
	startRun(t, m, "mock", 1)
	before := len(m.run.Items())
	m.Update(eventMsg{gen: old, ev: event.Log{Level: "info", Msg: "late"}})
	if len(m.run.Items()) != before {
		t.Fatal("event from an old run leaked into the new one")
	}
}

// TestProgram drives a real bubbletea program headless: type a run command,
// let the launcher stream the demo script through program.Send, then quit.
func TestProgram(t *testing.T) {
	finished := make(chan struct{})
	opts := testOptions()
	opts.NoAnim = false
	opts.Launch = func(ctx context.Context, path, network string, sink event.Sink) error {
		defer close(finished)
		for _, ev := range demo.Events(path, network) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			sink(ev)
			time.Sleep(time.Millisecond)
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
		for _, r := range "run token-flow" {
			p.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		p.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
		select {
		case <-finished:
			time.Sleep(100 * time.Millisecond)
		case <-time.After(5 * time.Second):
			t.Error("launcher never finished")
		}
		p.Quit()
	}()

	final, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	fm := final.(*Model)
	if fm.run == nil || fm.run.Result() == nil {
		t.Fatal("run did not finish inside the program")
	}
	if ok, failed, _ := fm.run.AssertionCounts(); ok != 5 || failed != 1 {
		t.Fatalf("assertions = %d ok, %d failed", ok, failed)
	}
}

func TestLayoutSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {119, 40}, {120, 30}, {160, 50}, {200, 60}} {
		w, h := size[0], size[1]
		m := newTestModel(w, h)
		checkBounds(t, m, m.View().Content)
		startRun(t, m, "testnet", -1)
		checkBounds(t, m, m.View().Content)

		if w >= compactWidth && h >= compactHeight {
			if m.compact {
				t.Errorf("%dx%d should not be compact", w, h)
			}
			if got := m.layout.sidebar.Dx() + 1; got != sidebarWidth {
				t.Errorf("%dx%d sidebar is %d wide with its gap, want %d", w, h, got, sidebarWidth)
			}
			if m.layout.sidebar.Max.X != w-1 {
				t.Errorf("%dx%d sidebar ends at %d, want %d", w, h, m.layout.sidebar.Max.X, w-1)
			}
		}
	}
}
