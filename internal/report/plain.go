// Package report prints runs for terminals and machines.
package report

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

// palette follows the charmtone colors the tui uses
var (
	cPrimary = lipgloss.Color("#6B50FF")
	cBase    = lipgloss.Color("#ECEBF0")
	cMuted   = lipgloss.Color("#858392")
	cSubtle  = lipgloss.Color("#605F6B")
	cLine    = lipgloss.Color("#3A3943")
	cGreen   = lipgloss.Color("#00FFB2")
	cGreenBg = lipgloss.Color("#12C78F")
	cRed     = lipgloss.Color("#EB4268")
	cRedBg   = lipgloss.Color("#FF577D")
	cBlue    = lipgloss.Color("#00A4FF")
	cOrange  = lipgloss.Color("#FF985A")
	cDark    = lipgloss.Color("#201F26")
)

var (
	sBase    = lipgloss.NewStyle().Foreground(cBase)
	sMuted   = lipgloss.NewStyle().Foreground(cMuted)
	sSubtle  = lipgloss.NewStyle().Foreground(cSubtle)
	sName    = lipgloss.NewStyle().Foreground(cBlue)
	sOK      = lipgloss.NewStyle().Foreground(cGreen).SetString("✓")
	sFail    = lipgloss.NewStyle().Foreground(cRed).SetString("×")
	sSkip    = lipgloss.NewStyle().Foreground(cSubtle).SetString("○")
	sDot     = lipgloss.NewStyle().Foreground(cGreenBg).SetString("●")
	sLogo    = lipgloss.NewStyle().Foreground(cDark).Background(cPrimary).Bold(true).Padding(0, 1)
	sPass    = lipgloss.NewStyle().Foreground(cDark).Background(cGreenBg).Bold(true).Padding(0, 1).SetString("PASS")
	sFailTag = lipgloss.NewStyle().Foreground(cDark).Background(cRedBg).Bold(true).Padding(0, 1).SetString("FAIL")
	sSkipTag = lipgloss.NewStyle().Foreground(cDark).Background(cSubtle).Bold(true).Padding(0, 1).SetString("SKIP")
	sWarn    = lipgloss.NewStyle().Foreground(cOrange)
)

// Plain streams a run as readable lines. It is safe to use as an event.Sink.
type Plain struct {
	w     io.Writer
	width int
	mu    sync.Mutex

	section string
	started map[int]event.StepStarted
	tools   map[string]event.AgentTool
	live    bool // a streamed agent text line is still open
}

func (p *Plain) endLive() {
	if p.live {
		p.live = false
		p.println("")
	}
}

func NewPlain(w io.Writer, width int) *Plain {
	if width <= 0 {
		width = 100
	}
	return &Plain{w: w, width: min(width, 120), started: map[int]event.StepStarted{}, tools: map[string]event.AgentTool{}}
}

func (p *Plain) Sink(e event.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e := e.(type) {
	case event.RunStarted:
		head := sLogo.Render("hh") + " " + sBase.Bold(true).Render(e.Scenario) +
			sMuted.Render(fmt.Sprintf(" · %s · operator %s · run %s", e.Network, e.Operator, e.RunID))
		p.println("")
		p.println(" " + head)
	case event.ActorReady:
		p.enter("Actors")
		line := fmt.Sprintf("   %s %s %s %s", sDot, pad(sName.Render(e.Name), 14), pad(sBase.Render(e.Account), 12), sMuted.Render(e.KeyType))
		if e.Hbar != "" {
			line += sMuted.Render(" · " + e.Hbar)
		}
		p.println(line)
	case event.StepStarted:
		p.started[e.Index] = e
	case event.StepFinished:
		p.enter("Steps")
		p.step(e)
	case event.AssertionFinished:
		p.enter("Assertions")
		p.assertion(e)
	case event.Log:
		style := sMuted
		if e.Level != "info" {
			style = sWarn
		}
		p.println("   " + style.Render("⋯ "+e.Msg))
	case event.RunFinished:
		p.finish(e)
	case event.AgentStarted:
		p.agentStarted(e)
	case event.AgentTextDelta:
		// print pieces as they come, the final AgentText then only ends the line.
		// each line is styled on its own so lipgloss does not pad them to one width
		text := e.Text
		if !p.live {
			text = strings.TrimLeft(text, " \n")
			lipgloss.Fprint(p.w, "   ")
			p.live = true
		}
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				lipgloss.Fprint(p.w, "\n   ")
			}
			if line != "" {
				lipgloss.Fprint(p.w, sBase.Render(line))
			}
		}
	case event.AgentText:
		if p.live {
			p.live = false
			p.println("")
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(e.Text), "\n") {
			p.println(truncate("   "+sBase.Render(line), p.width))
		}
	case event.AgentTool:
		p.endLive()
		p.tools[e.ID] = e
	case event.AgentToolResult:
		p.toolResult(e)
	case event.AgentFinished:
		p.endLive()
		p.agentFinished(e)
	case event.JudgeFinished:
		p.judgeFinished(e)
	case event.LoopFinished:
		p.loopFinished(e)
	}
}

func (p *Plain) agentStarted(e event.AgentStarted) {
	title := fmt.Sprintf("Attempt %d · %s", e.Attempt, e.Agent)
	if e.Repair {
		title += " · repair"
	}
	p.section = ""
	p.enter(title)
	prompt := strings.TrimSpace(e.Prompt)
	lines := strings.Split(prompt, "\n")
	if e.Repair && len(lines) > 6 {
		lines = append(lines[:6], fmt.Sprintf("… %d more lines", len(lines)-6))
	}
	bar := lipgloss.NewStyle().Foreground(cPrimary).Render("│")
	for _, l := range lines {
		p.println(truncate("  "+bar+" "+sBase.Render(l), p.width))
	}
	p.println("")
}

func (p *Plain) toolResult(e event.AgentToolResult) {
	call := p.tools[e.ID]
	name := call.Name
	if name == "" {
		name = e.Name
	}
	icon := sOK.String()
	if e.IsError {
		icon = sFail.String()
	}
	p.println(truncate(fmt.Sprintf("   %s %s %s", icon, sName.Render(name), sMuted.Render(call.Summary)), p.width))
	out := strings.Split(strings.TrimRight(e.Output, "\n"), "\n")
	if strings.TrimSpace(e.Output) == "" {
		return
	}
	limit := 3
	if e.IsError {
		limit = 8
	}
	for i, l := range out {
		if i == limit {
			p.println("     " + sSubtle.Render(fmt.Sprintf("… %d more lines", len(out)-limit)))
			break
		}
		p.println(truncate("     "+sSubtle.Render(l), p.width))
	}
}

func (p *Plain) agentFinished(e event.AgentFinished) {
	info := fmt.Sprintf("◇ %s · %d turns · $%.2f · %s", orDash(e.Model), e.Turns, e.CostUSD, short(e.Elapsed))
	p.println("")
	p.println("   " + sMuted.Render(info))
	if e.Error != "" {
		p.println("   " + sFailTag.String() + " " + lipgloss.NewStyle().Foreground(cRed).Render(e.Error))
	}
}

func (p *Plain) judgeFinished(e event.JudgeFinished) {
	p.println("")
	total := e.Passed + e.Failed
	if e.Status == event.Passed {
		p.println(fmt.Sprintf(" %s %s", sPass.String(), sBase.Render(fmt.Sprintf("judge passed %d/%d scenarios", e.Passed, total))))
		return
	}
	p.println(fmt.Sprintf(" %s %s", sFailTag.String(), sBase.Render(fmt.Sprintf("judge failed %d/%d scenarios, sending the findings back", e.Failed, total))))
	for _, f := range e.Findings {
		p.println(truncate("     "+sMuted.Render("- "+f), p.width))
	}
}

func (p *Plain) loopFinished(e event.LoopFinished) {
	p.println("")
	summary := fmt.Sprintf("in %d attempt(s) · $%.2f · %s", e.Attempts, e.CostUSD, short(e.Elapsed))
	if e.Status == event.Passed {
		p.println(fmt.Sprintf(" %s %s %s", sOK, lipgloss.NewStyle().Foreground(cGreen).Render("done"), sMuted.Render(summary)))
		if e.SessionID != "" {
			p.println("   " + sSubtle.Render("continue this conversation: hh agent --resume "+e.SessionID+" \"...\""))
		}
	} else {
		p.println(fmt.Sprintf(" %s %s %s", sFail, lipgloss.NewStyle().Foreground(cRed).Render("gave up"), sMuted.Render(summary)))
		if e.Error != "" {
			p.println("   " + lipgloss.NewStyle().Foreground(cRed).Render(e.Error))
		}
	}
	p.println("")
}

func orDash(s string) string {
	if s == "" {
		return "agent"
	}
	return s
}

func (p *Plain) enter(section string) {
	if p.section == section {
		return
	}
	p.section = section
	p.println("")
	rule := strings.Repeat("─", max(0, p.width-lipgloss.Width(section)-3))
	p.println(" " + sSubtle.Render(section) + " " + lipgloss.NewStyle().Foreground(cLine).Render(rule))
}

func (p *Plain) step(e event.StepFinished) {
	st := p.started[e.Index]
	icon := sOK.String()
	switch e.Status {
	case event.Failed:
		icon = sFail.String()
	case event.Skipped:
		icon = sSkip.String()
	}
	head := fmt.Sprintf("   %s %s", icon, sName.Render(e.Op))
	if st.Target != "" {
		head += " " + sBase.Render(st.Target)
	}
	if len(st.Params) > 0 {
		var kv []string
		for _, pr := range st.Params {
			kv = append(kv, pr.Key+"="+pr.Value)
		}
		head += sSubtle.Render(" (" + strings.Join(kv, ", ") + ")")
	}
	if e.Elapsed > 0 {
		head += sSubtle.Render(" " + short(e.Elapsed))
	}
	p.println(truncate(head, p.width))

	var detail []string
	switch {
	case e.Status == event.Skipped:
		detail = append(detail, sSubtle.Render("skipped after an earlier failure"))
	case e.Status == event.Failed && e.Error != "":
		detail = append(detail, lipgloss.NewStyle().Foreground(cRed).Render(e.Error))
	case e.Expected != "" && e.Expected != "SUCCESS":
		detail = append(detail, sMuted.Render("expected ")+sBase.Render(e.Receipt))
	}
	for _, ent := range e.Entities {
		detail = append(detail, sMuted.Render(ent.Key+" = ")+sBase.Render(ent.Value))
	}
	if e.TxID != "" && e.Status != event.Skipped {
		detail = append(detail, sSubtle.Render("tx "+e.TxID))
	}
	if len(detail) > 0 {
		p.println(truncate("     "+strings.Join(detail, sSubtle.Render(" · ")), p.width))
	}
	if e.Link != "" {
		p.println("     " + sSubtle.Hyperlink(e.Link).Render(e.Link))
	}
}

func (p *Plain) assertion(e event.AssertionFinished) {
	tag := sPass.String()
	switch e.Status {
	case event.Failed:
		tag = sFailTag.String()
	case event.Skipped:
		tag = sSkipTag.String()
	}
	line := fmt.Sprintf("   %s %s %s", tag, pad(sBase.Render(e.Title), 30), pad(sMuted.Render(e.Expected), 22))
	if e.Status != event.Skipped {
		line += sMuted.Render(" actual ") + sBase.Render(e.Actual)
	}
	p.println(truncate(line, p.width))
	if e.Status == event.Failed {
		if e.Error != "" {
			p.println("        " + lipgloss.NewStyle().Foreground(cRed).Render(e.Error))
		}
		if e.Source != "" {
			p.println("        " + sSubtle.Render(fmt.Sprintf("%s (%d reads)", e.Source, e.Attempts)))
		}
	}
}

func (p *Plain) finish(e event.RunFinished) {
	p.println("")
	icon, word := sOK.String(), lipgloss.NewStyle().Foreground(cGreen).Render("passed")
	if e.Status == event.Failed {
		icon, word = sFail.String(), lipgloss.NewStyle().Foreground(cRed).Render("failed")
	}
	summary := fmt.Sprintf(" %s %s %s", icon, word, sMuted.Render(fmt.Sprintf("· %d/%d steps · %d/%d assertions · %s",
		e.StepsOK, e.StepsOK+e.StepsFail, e.AssertOK, e.AssertOK+e.AssertFail, short(e.Elapsed))))
	p.println(summary)
	if e.Error != "" {
		for _, l := range strings.Split(e.Error, "\n") {
			p.println("   " + lipgloss.NewStyle().Foreground(cRed).Render(l))
		}
	}
	p.println("")
}

func (p *Plain) println(s string) { lipgloss.Fprintln(p.w, s) }

func pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func truncate(s string, w int) string { return ansi.Truncate(s, w, "…") }

func short(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
