package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/logo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/run"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

// networkLine renders "◇ mock  operator 0.0.2".
func (m *Model) networkLine(network string, width int) string {
	operator := m.operator
	if operator == "" {
		operator = "not set"
	}
	line := m.sty.Subtle.Render(styles.IconInfo) + " " + m.sty.NetworkPill(network) + " " +
		m.sty.Subtle.Render("operator ") + m.sty.Muted.Render(operator)
	return ansi.Truncate(line, width, "…")
}

func (m *Model) landingView(width, height int) string {
	sty := m.sty
	info := []string{
		sty.Muted.Render(ansi.Truncate(prettyPath(m.opts.Cwd), width, "…")),
		"",
		m.networkLine(m.network, width),
		"  " + sty.Subtle.Render(fmt.Sprintf("%d scenarios %s %s", len(m.opts.Scenarios), styles.Dot, networkBlurb(m.network))),
	}
	if m.opts.Agent != nil || len(m.judges) > 0 {
		info = append(info, m.agentLine(width))
	}

	colW := min(30, (width-2)/3)
	rows := max(1, height-len(info)-5)
	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		m.scenarioColumn(colW, rows), " ",
		m.networkColumn(colW, rows), " ",
		m.lastRunColumn(colW),
	)

	body := lipgloss.JoinVertical(lipgloss.Left, append(info, "", columns)...)
	return lipgloss.NewStyle().PaddingTop(1).MaxHeight(height).Render(body)
}

// agentLine renders "◇ claude  judges token-flow, topic-submit".
func (m *Model) agentLine(width int) string {
	sty := m.sty
	line := sty.Subtle.Render(styles.IconInfo) + " " + sty.Base.Render(m.opts.AgentName) + "  "
	if len(m.judges) == 0 {
		line += sty.Subtle.Render("no judges, add one with judge <scenario>")
	} else {
		line += sty.Subtle.Render("judges ") + sty.Muted.Render(strings.Join(m.judgeNames(), ", "))
	}
	return ansi.Truncate(line, width, "…")
}

func (m *Model) scenarioColumn(width, rows int) string {
	sty := m.sty
	lines := []string{sty.Rule("Scenarios", width), ""}
	if len(m.opts.Scenarios) == 0 {
		return strings.Join(append(lines, sty.Subtle.Render("None")), "\n")
	}
	shown := m.opts.Scenarios
	more := 0
	if len(shown) > rows {
		shown, more = shown[:max(0, rows-1)], len(shown)-rows+1
	}
	for _, s := range shown {
		row := sty.Run.IconPending.Render(styles.IconPending) + " " + sty.Base.Foreground(styles.FgSubtle).Render(s.Name)
		if slices.Contains(m.judges, s.Path) {
			row += " " + sty.Logo.Meta.Render("judge")
		} else if s.Steps > 0 {
			row += " " + sty.Subtle.Render(fmt.Sprintf("%d steps", s.Steps))
		}
		lines = append(lines, ansi.Truncate(row, width, "…"))
	}
	if more > 0 {
		lines = append(lines, sty.Subtle.Render(fmt.Sprintf("…and %d more", more)))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m *Model) networkColumn(width, rows int) string {
	sty := m.sty
	lines := []string{sty.Rule("Network", width), ""}
	for i, n := range m.opts.Networks {
		if i >= rows {
			break
		}
		var row string
		if n == m.network {
			row = lipgloss.NewStyle().Foreground(styles.Secondary).Render(styles.RadioOn) + " " + sty.Base.Render(n)
		} else {
			row = sty.Subtle.Render(styles.RadioOff) + " " + sty.Muted.Render(n)
		}
		row += " " + sty.Subtle.Render(networkBlurb(n))
		lines = append(lines, ansi.Truncate(row, width, "…"))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m *Model) lastRunColumn(width int) string {
	sty := m.sty
	lines := []string{sty.Rule("Last run", width), ""}
	if m.last == nil {
		return strings.Join(append(lines, sty.Subtle.Render("None")), "\n")
	}
	r := m.last.result
	icon := sty.Run.IconSuccess.Render(styles.IconSuccess)
	if r.Status != event.Passed {
		icon = sty.Run.IconError.Render(styles.IconError)
	}
	lines = append(lines,
		ansi.Truncate(icon+" "+sty.Base.Foreground(styles.FgSubtle).Render(m.last.name), width, "…"),
		"  "+sty.Muted.Render(fmt.Sprintf("%d/%d steps %s %d/%d checks", r.StepsOK, m.last.steps, styles.Dot, r.AssertOK, m.last.asserts)),
		"  "+sty.Subtle.Render(strings.TrimSpace(fmt.Sprintf("%s %s %s", m.last.network, styles.Dot, elapsedText(r)))),
	)
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func elapsedText(r event.RunFinished) string {
	if r.Elapsed <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1fs", r.Elapsed.Seconds())
}

// sidebarView is the right column of the wide run view.
func (m *Model) sidebarView(width, height int) string {
	if m.session != nil {
		return m.sessionSidebar(width, height)
	}
	sty := m.sty
	r := m.run
	w := max(1, width-2)

	blocks := []string{
		logo.Compact(sty, m.opts.Version, w),
		"",
		sty.Sidebar.Title.Render(ansi.Truncate(r.Name(), w, "…")),
		sty.Sidebar.Info.Render(ansi.Truncate(runRef(r.Info.RunID, r.Path), w, "…")),
		"",
		m.networkLine(r.Network, w),
		"",
		m.actorsSection(r, w, 0),
		"",
		m.progressSection(r, w),
		"",
		m.assertionsSection(r, w),
	}
	if len(m.judges) > 0 {
		blocks = append(blocks, "", m.judgesSection(m.selectedJudges(), w))
	}
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(strings.Join(blocks, "\n"))
}

func (m *Model) sessionSidebar(width, height int) string {
	sty := m.sty
	s := m.session
	w := max(1, width-2)

	blocks := []string{
		logo.Compact(sty, m.opts.Version, w),
		"",
		m.promptTitle(w, 2),
		sty.Sidebar.Info.Render(ansi.Truncate(m.agentRef(), w, "…")),
		"",
		m.networkLine(s.Network, w),
		"",
		m.agentSection(w),
		"",
		m.judgesSection(s.Judges(), w),
	}
	if r := s.Current(); r != nil {
		blocks = append(blocks, "", m.progressSection(r, w), "", m.assertionsSection(r, w))
	}
	return lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(strings.Join(blocks, "\n"))
}

// promptTitle is the session prompt wrapped to at most rows lines.
func (m *Model) promptTitle(width, rows int) string {
	text := strings.Join(strings.Fields(m.session.Prompt), " ")
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(lines[rows-1]+" …", width, "…")
	}
	return m.sty.Sidebar.Title.Render(strings.Join(lines, "\n"))
}

// agentRef is "claude · claude-sonnet-4-5", the model once it is known.
func (m *Model) agentRef() string {
	s := m.session
	if s.Model() == "" {
		return s.Agent
	}
	return s.Agent + " " + styles.Dot + " " + s.Model()
}

func (m *Model) selectedJudges() []run.Judge {
	judges := make([]run.Judge, len(m.judges))
	for i, path := range m.judges {
		judges[i] = run.Judge{Name: m.scenarioName(path), Path: path}
	}
	return judges
}

func (m *Model) agentSection(width int) string {
	sty := m.sty
	s := m.session
	lines := []string{sty.Rule("Agent", width)}

	res := s.Result()
	passed := res != nil && res.Status == event.Passed
	failed := res != nil && res.Status != event.Passed && !s.Canceled()
	icon := sty.Icon(passed, failed, !s.Done())

	label := "starting"
	if s.Attempt() > 0 {
		label = fmt.Sprintf("attempt %d", s.Attempt())
	}
	stage := s.Stage()
	if s.Canceled() {
		stage = "canceled"
	} else if res != nil {
		stage = string(res.Status)
	}
	if stage == label {
		stage = ""
	}
	pad := max(0, width-2-lipgloss.Width(label)-lipgloss.Width(stage)-1)
	lines = append(lines, icon+" "+sty.Base.Foreground(styles.FgSubtle).Render(label+strings.Repeat(" ", pad))+" "+sty.Muted.Render(stage))

	usage := fmt.Sprintf("%d turns %s $%.2f", s.Turns(), styles.Dot, s.Cost())
	lines = append(lines, "  "+sty.Subtle.Render(ansi.Truncate(usage, max(0, width-2), "…")))
	return strings.Join(lines, "\n")
}

func (m *Model) judgesSection(judges []run.Judge, width int) string {
	sty := m.sty
	lines := []string{sty.Rule("Judges", width)}
	if len(judges) == 0 {
		return strings.Join(append(lines, sty.Subtle.Render("None")), "\n")
	}
	for _, j := range judges {
		icon := sty.Icon(j.Status == event.Passed, j.Status == event.Failed, j.Status == event.Running)
		row := icon + " " + sty.Base.Foreground(styles.FgSubtle).Render(j.Name)
		lines = append(lines, ansi.Truncate(row, width, "…"))
	}
	return strings.Join(lines, "\n")
}

func runRef(id, path string) string {
	if id == "" {
		return path
	}
	return "run " + id + " " + styles.Dot + " " + path
}

func (m *Model) actorsSection(r *run.Run, width, limit int) string {
	sty := m.sty
	actors, want := r.Actors()
	lines := []string{sty.Rule("Actors", width)}
	if len(actors) == 0 && want == 0 {
		return strings.Join(append(lines, sty.Subtle.Render("None")), "\n")
	}
	nameW := 0
	for _, a := range actors {
		nameW = max(nameW, lipgloss.Width(a.Name))
	}
	for i, a := range actors {
		if limit > 0 && i >= limit-1 && len(actors) > limit {
			lines = append(lines, sty.Subtle.Render(fmt.Sprintf("…and %d more", len(actors)-i)))
			break
		}
		row := sty.Run.IconSuccess.Render(styles.IconSuccess) + " " +
			sty.Base.Foreground(styles.FgSubtle).Render(fmt.Sprintf("%-*s", nameW, a.Name)) + " " +
			sty.Muted.Render(a.Account)
		lines = append(lines, ansi.Truncate(row, width, "…"))
	}
	if missing := want - len(actors); missing > 0 && !r.Done() {
		lines = append(lines, sty.Run.IconPending.Render(styles.IconPending)+" "+sty.Subtle.Render(fmt.Sprintf("%d being created", missing)))
	}
	return strings.Join(lines, "\n")
}

type stage struct {
	name        string
	done, total int
	failed      int
	started     bool
}

func stages(r *run.Run) []stage {
	actors, want := r.Actors()
	sOK, sFail, sTotal := r.StepCounts()
	aOK, aFail, aTotal := r.AssertionCounts()
	stageName := r.Stage()
	return []stage{
		{name: "actors", done: len(actors), total: want, started: stageName != "starting"},
		{name: "steps", done: sOK + sFail, total: sTotal, failed: sFail, started: sOK+sFail > 0 || stageName == "steps" || stageName == "assertions"},
		{name: "assertions", done: aOK + aFail, total: aTotal, failed: aFail, started: aOK+aFail > 0 || stageName == "assertions"},
	}
}

func (m *Model) stageIcon(r *run.Run, s stage) string {
	complete := s.done >= s.total && s.total > 0
	return m.sty.Icon(complete && s.failed == 0, s.failed > 0, s.started && !complete && !r.Done())
}

func (m *Model) progressSection(r *run.Run, width int) string {
	sty := m.sty
	lines := []string{sty.Rule("Progress", width)}
	for _, s := range stages(r) {
		count := fmt.Sprintf("%d/%d", s.done, s.total)
		label := fmt.Sprintf("%-*s", max(0, width-2-lipgloss.Width(count)-1), s.name)
		lines = append(lines, m.stageIcon(r, s)+" "+sty.Base.Foreground(styles.FgSubtle).Render(label)+" "+sty.Muted.Render(count))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) assertionsSection(r *run.Run, width int) string {
	sty := m.sty
	ok, failed, total := r.AssertionCounts()
	waiting := max(0, total-ok-failed)
	line := sty.Run.IconSuccess.Render(styles.IconSuccess) + " " + sty.Muted.Render(fmt.Sprint(ok)) + "  " +
		sty.Run.IconError.Render(styles.IconError) + " " + sty.Muted.Render(fmt.Sprint(failed)) + "  " +
		sty.Run.IconIdle.Render(styles.IconIdle) + " " + sty.Muted.Render(fmt.Sprint(waiting))
	return sty.Rule("Assertions", width) + "\n" + line
}

// compactHeader is the one row header that replaces the sidebar on small
// terminals.
func (m *Model) compactHeader(width int) string {
	sty := m.sty
	name := " " + logo.Name(sty) + " "

	var parts []string
	if s := m.session; s != nil {
		parts = append(parts, sty.Header.Detail.Render(s.Agent), sty.Header.Detail.Render(s.Network),
			sty.Header.Detail.Render(m.sessionProgressText()))
		if n := len(s.Judges()); n > 0 {
			parts = append(parts, sty.Header.Detail.Render(plural(n, "judge")))
		}
	} else {
		r := m.run
		parts = append(parts, sty.Header.Detail.Render(r.Name()), sty.Header.Detail.Render(r.Network),
			sty.Header.Detail.Render(progressText(r)))
	}
	tip := " open"
	if m.detailsOpen {
		tip = " close"
	}
	parts = append(parts, sty.Header.Keystroke.Render("ctrl+d")+sty.Header.Tip.Render(tip))
	details := strings.Join(parts, sty.Header.Separator.Render(" • "))

	const minDiagonals = 3
	avail := width - lipgloss.Width(name) - minDiagonals - 2
	details = ansi.Truncate(details, max(0, avail), "…")
	diagonals := max(minDiagonals, width-lipgloss.Width(name)-lipgloss.Width(details)-2)
	return ansi.Truncate(name+sty.Header.Diagonals.Render(strings.Repeat(styles.Diagonal, diagonals))+" "+details, width, "")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func progressText(r *run.Run) string {
	if res := r.Result(); res != nil {
		status := "passed"
		switch {
		case r.Canceled():
			status = "canceled"
		case res.Status != event.Passed:
			status = "failed"
		}
		_, _, total := r.AssertionCounts()
		return fmt.Sprintf("%s %d/%d", status, res.AssertOK, total)
	}
	for _, s := range stages(r) {
		if s.name == r.Stage() {
			return fmt.Sprintf("%s %d/%d", s.name, s.done, s.total)
		}
	}
	return r.Stage()
}

// sessionProgressText is "attempt 2 working", "attempt 1 judging steps 3/8"
// or the loop outcome.
func (m *Model) sessionProgressText() string {
	s := m.session
	if res := s.Result(); res != nil {
		switch {
		case s.Canceled():
			return "canceled"
		case res.Status == event.Passed:
			return "passed in " + plural(res.Attempts, "attempt")
		}
		return "failed after " + plural(res.Attempts, "attempt")
	}
	if s.Attempt() == 0 {
		return "starting"
	}
	text := fmt.Sprintf("attempt %d %s", s.Attempt(), s.Stage())
	if r := s.Current(); r != nil && s.Stage() == "judging" {
		text += " " + progressText(r)
	}
	return text
}

// detailsView is the ctrl+d overlay in compact mode, the sidebar content
// laid out in columns.
func (m *Model) detailsView(width, height int) string {
	sty := m.sty
	inner := max(1, width-sty.Details.GetHorizontalFrameSize())
	colW := max(1, min(40, (inner-4)/3))

	var head []string
	if s := m.session; s != nil {
		head = []string{
			m.promptTitle(inner, 1),
			sty.Sidebar.Info.Render(ansi.Truncate(m.agentRef(), inner, "…")),
			"",
			m.networkLine(s.Network, inner),
			"",
		}
	} else {
		r := m.run
		head = []string{
			sty.Sidebar.Title.Render(ansi.Truncate(r.Name(), inner, "…")) + " " +
				sty.Sidebar.Info.Render(ansi.Truncate(runRef(r.Info.RunID, r.Path), max(0, inner-lipgloss.Width(r.Name())-1), "…")),
			"",
			m.networkLine(r.Network, inner),
			"",
		}
	}
	rows := max(1, height-sty.Details.GetVerticalFrameSize()-len(head)-3)

	var columns []string
	if s := m.session; s != nil {
		progress := sty.Rule("Progress", colW) + "\n" + sty.Subtle.Render("No judge run yet")
		if r := s.Current(); r != nil {
			progress = m.progressSection(r, colW)
		}
		columns = []string{m.agentSection(colW), m.judgesSection(s.Judges(), colW), progress}
	} else {
		r := m.run
		columns = []string{m.actorsSection(r, colW, rows-1), m.progressSection(r, colW), m.assertionsSection(r, colW)}
	}

	col := lipgloss.NewStyle().Width(colW).MaxHeight(rows)
	sections := lipgloss.JoinHorizontal(lipgloss.Top,
		col.Render(columns[0]), "  ",
		col.Render(columns[1]), "  ",
		col.Render(columns[2]),
	)
	version := lipgloss.NewStyle().Foreground(styles.Separator).Width(inner).Align(lipgloss.Right).Render(m.opts.Version)

	body := lipgloss.JoinVertical(lipgloss.Left, append(head, sections, version)...)
	return sty.Details.Width(width).Render(body)
}
