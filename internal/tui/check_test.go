package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

const goTest = "go test ./..."

// newProjectModel is an agent model started the way the cli starts it when
// an hh.yaml declares a judge and a check.
func newProjectModel(w, h int) *Model {
	opts := testOptions()
	opts.AgentName = "claude"
	opts.Agent = func(context.Context, AgentRequest, event.Sink) error { return nil }
	opts.Judges = []string{scenarioPath}
	opts.Checks = []Check{{Run: goTest}}
	opts.Project = "/work/hh-demo/hh.yaml"
	m := newModel(context.Background(), opts)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestPreloadFromOptions(t *testing.T) {
	m := newProjectModel(120, 32)
	if !slices.Equal(m.judges, []string{scenarioPath}) {
		t.Fatalf("judges = %v", m.judges)
	}
	if len(m.checks) != 1 || m.checks[0].Name != goTest || m.checks[0].Run != goTest {
		t.Fatalf("checks = %+v, the name should default to the command", m.checks)
	}
	if got := m.projectLabel(); got != "hh.yaml" {
		t.Fatalf("project label = %q", got)
	}
	requireGolden(t, m)
}

func TestCheckCommands(t *testing.T) {
	m := newAgentModel(120, 32)

	typeText(m, "/check go build ./...")
	press(m, "enter")
	if len(m.checks) != 1 || m.checks[0] != (Check{Name: "go build ./...", Run: "go build ./..."}) || m.session != nil {
		t.Fatalf("/check should add a check, checks %+v", m.checks)
	}

	typeText(m, "/check go build ./...")
	press(m, "enter")
	if len(m.checks) != 1 || m.toast.kind != toastWarn {
		t.Fatal("the same command twice should be refused")
	}

	addJudges(m, "token-flow")
	typeText(m, "/judge off")
	press(m, "enter")
	if len(m.judges) != 0 || len(m.checks) != 1 {
		t.Fatal("/judge off should only clear scenarios")
	}

	press(m, "ctrl+p")
	typeText(m, "clear checks")
	press(m, "enter")
	if m.dialog.HasDialogs() || len(m.checks) != 0 {
		t.Fatal("the palette should clear the checks")
	}

	press(m, "ctrl+p")
	if strings.Contains(m.View().Content, "Clear checks") {
		t.Fatal("clear checks should not be offered without checks")
	}
	press(m, "esc")

	typeText(m, "/check yarn hardhat:test")
	press(m, "enter")
	typeText(m, "/check clear")
	press(m, "enter")
	if len(m.checks) != 0 {
		t.Fatal("/check clear did not clear the checks")
	}
	typeText(m, "/check off")
	press(m, "enter")
	if m.toast.kind != toastWarn {
		t.Fatal("clearing no checks should warn")
	}

	typeText(m, "check go vet ./...")
	press(m, "enter")
	if len(m.checks) != 0 || m.session == nil || m.session.Prompt != "check go vet ./..." {
		t.Fatal("check without a slash is a prompt")
	}
}

func TestAgentRequestChecks(t *testing.T) {
	m := newProjectModel(120, 32)
	reqs := recordAgent(m)
	converse(t, m, reqs, "add a payout helper")

	typeText(m, "/check go vet ./...")
	press(m, "enter")
	converse(t, m, reqs, "now log every payout")

	if len(*reqs) != 2 {
		t.Fatalf("requests = %+v", *reqs)
	}
	first, second := (*reqs)[0], (*reqs)[1]
	if !slices.Equal(first.Judges, []string{scenarioPath}) || !slices.Equal(first.Checks, []Check{{Name: goTest, Run: goTest}}) {
		t.Fatalf("first request = %+v", first)
	}
	if len(second.Checks) != 2 || second.Checks[1].Run != "go vet ./..." {
		t.Fatalf("second request checks = %+v", second.Checks)
	}
	if &first.Checks[0] == &m.checks[0] {
		t.Fatal("the request shares the check list with the model")
	}
}

func TestAgentCheckRunning(t *testing.T) {
	m := newProjectModel(120, 32)
	startAgent(t, m, func(ev event.Event) bool {
		_, ok := ev.(event.CheckStarted)
		return ok
	})
	s := m.session
	if s.Stage() != "judging" || s.RunningCheck() != goTest {
		t.Fatalf("stage %q running check %q", s.Stage(), s.RunningCheck())
	}
	if c := s.Checks(); len(c) != 1 || c[0].Status != event.Running {
		t.Fatalf("checks = %+v", c)
	}
	requireGolden(t, m)
}

func TestAgentCheckFailed(t *testing.T) {
	m := newProjectModel(120, 32)
	startAgent(t, m, func(ev event.Event) bool {
		_, ok := ev.(event.CheckFinished)
		return ok
	})
	if c := m.session.Checks(); c[0].Status != event.Failed {
		t.Fatalf("checks = %+v", c)
	}
	text := listText(m)
	for _, want := range []string{"× check go test ./... exit 1 · 3.2s", "… (2 lines hidden)", "FAIL: TestPayoutExecutes"} {
		if !strings.Contains(text, want) {
			t.Errorf("list is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ok    github.com/acme/treasury/internal/config") {
		t.Error("check output should keep its last lines, not its first")
	}
	requireGolden(t, m)
}

func TestAgentChecksFinished(t *testing.T) {
	m := newProjectModel(120, 32)
	startAgent(t, m, nil)
	s := m.session
	if s.Result() == nil || s.Result().Status != event.Passed || s.Result().Attempts != 2 {
		t.Fatalf("loop result = %+v", s.Result())
	}
	if c := s.Checks(); len(c) != 1 || c[0].Status != event.Passed {
		t.Fatalf("checks = %+v", c)
	}
	text := listText(m)
	for _, want := range []string{"judge failed · 1 check, 0 scenarios", "judge passed · 1 check, 1 scenario", `check "go test ./..." (`} {
		if !strings.Contains(text, want) {
			t.Errorf("list is missing %q", want)
		}
	}
	if strings.Contains(text, "output:") {
		t.Error("findings should not repeat the check output")
	}
	if i, j := strings.Index(text, "check go test"), strings.Index(text, "Judge token-flow"); i < 0 || j < 0 || i > j {
		t.Error("checks should come before the judge scenarios")
	}
	requireGolden(t, m)
}

func TestCompactChecks(t *testing.T) {
	m := newProjectModel(90, 28)
	startAgent(t, m, func(ev event.Event) bool {
		_, ok := ev.(event.CheckStarted)
		return ok
	})
	if !strings.Contains(m.View().Content, "ctrl+d") {
		t.Fatal("the compact header lost its ctrl+d tip")
	}
	if !strings.Contains(ansi.Strip(m.compactHeader(200)), "1 judge • 1 check") {
		t.Fatalf("compact header should count the checks: %q", ansi.Strip(m.compactHeader(200)))
	}
	press(m, "ctrl+d")
	requireGolden(t, m)
}

func TestCheckLayoutSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {120, 32}, {200, 60}} {
		m := newProjectModel(size[0], size[1])
		m.checks = append(m.checks, Check{Name: "hardhat", Run: "yarn hardhat:test --network localhost --parallel"})
		checkBounds(t, m, m.View().Content)
		startAgent(t, m, nil)
		checkBounds(t, m, m.View().Content)
		if m.compact {
			press(m, "ctrl+d")
			checkBounds(t, m, m.View().Content)
		}
	}
}

func TestShare(t *testing.T) {
	for _, tc := range []struct {
		room      int
		want, got []int
	}{
		{100, []int{20, 30}, []int{20, 30}},
		{40, []int{10, 60}, []int{10, 30}},
		{40, []int{60, 60}, []int{20, 20}},
		{0, []int{5, 5}, []int{0, 0}},
	} {
		if got := share(tc.room, tc.want); !slices.Equal(got, tc.got) {
			t.Errorf("share(%d, %v) = %v, want %v", tc.room, tc.want, got, tc.got)
		}
	}
}
