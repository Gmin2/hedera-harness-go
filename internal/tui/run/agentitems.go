package run

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/anim"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

// maxOutputLines is how much tool output shows before the rest is folded.
const maxOutputLines = 6

// cleanText drops escape sequences and control characters that would break
// the layout, and expands tabs.
func cleanText(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r >= ' ' && r != 0x7f {
			return r
		}
		return -1
	}, s)
}

// renderLines styles each line on its own so short lines are not padded
// out to the widest one.
func renderLines(style lipgloss.Style, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = style.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func money(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// roundElapsed is elapsed rounded to whole seconds, the agent loop runs for
// minutes so tenths are noise.
func roundElapsed(d time.Duration) string {
	if d < time.Second {
		return elapsed(d)
	}
	return d.Round(time.Second).String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// footerRule renders "icon text ────" filling width, like the run footer.
func footerRule(sty *styles.Styles, icon lipgloss.Style, glyph, text string, width int) string {
	text = ansi.Truncate(text, max(0, width-4), "…")
	line := icon.Render(glyph) + " " + sty.Run.FooterText.Render(text)
	if rest := width - lipgloss.Width(line) - 1; rest > 0 {
		line += " " + sty.Section.Line.Render(strings.Repeat(styles.RuleChar, rest))
	}
	return line
}

// promptItem is what the user typed, with a thin bar on the left.
type promptItem struct {
	sty  *styles.Styles
	text string
}

func (p *promptItem) Render(width int) string {
	text := ansi.Wrap(strings.TrimSpace(cleanText(p.text)), max(1, contentWidth(width)), "")
	return p.sty.Run.Prompt.Render(text)
}

// textItem is what the agent said, as wrapped plain text. While live it
// grows with every delta and is wrapped again on each render.
type textItem struct {
	sty     *styles.Styles
	text    string
	attempt int
	live    bool
}

func (t *textItem) Render(width int) string {
	text := ansi.Wrap(cleanText(t.text), max(1, contentWidth(width)), "")
	return indent(renderLines(t.sty.Run.Text, text))
}

// toolItem is one tool call by the agent: a header with the tool name and
// what it acted on, then the first lines of its output.
type toolItem struct {
	sty    *styles.Styles
	call   event.AgentTool
	result *event.AgentToolResult
	anim   *anim.Anim
	halted bool
}

func (t *toolItem) Render(width int) string {
	w := contentWidth(width)
	sty := t.sty

	var icon string
	switch {
	case t.result != nil && t.result.IsError:
		icon = sty.Run.IconError.Render(styles.IconError)
	case t.result != nil:
		icon = sty.Run.IconSuccess.Render(styles.IconSuccess)
	case t.halted:
		icon = sty.Run.IconIdle.Render(styles.IconPending)
	default:
		icon = sty.Run.IconPending.Render(styles.IconPending)
	}
	name := t.call.Name
	if name == "" {
		name = "Tool"
	}
	prefix := icon + " " + sty.Run.Name.Render(name) + " "

	var suffix string
	switch {
	case t.spinning():
		suffix = " " + t.anim.Render()
	case t.result == nil:
		suffix = " " + sty.Run.Waiting.Render("Canceled.")
	}

	summary, _, _ := strings.Cut(strings.TrimSpace(cleanText(t.call.Summary)), "\n")
	room := w - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	header := prefix + sty.Muted.Render(ansi.Truncate(summary, max(0, room), "…")) + suffix
	header = ansi.Truncate(header, w, "…")

	body := t.body(w)
	if body == "" {
		return indent(header)
	}
	return indent(header + "\n\n" + body)
}

func (t *toolItem) body(w int) string {
	if t.result == nil {
		return ""
	}
	return outputBody(t.sty, t.result.Output, t.result.IsError, false, w)
}

// outputBody folds command output to maxOutputLines. Tool output keeps its
// head, check output keeps its tail since that is where failures end up.
func outputBody(sty *styles.Styles, output string, bad, tail bool, w int) string {
	out := strings.TrimRight(cleanText(output), " \n")
	out = strings.TrimLeft(out, "\n")
	if out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	hidden := 0
	if len(lines) > maxOutputLines {
		hidden = len(lines) - maxOutputLines
		if tail {
			lines = lines[hidden:]
		} else {
			lines = lines[:maxOutputLines]
		}
	}
	style := sty.Run.Output
	if bad {
		style = sty.Run.BodyBad
	}
	rows := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if l == "" {
			rows = append(rows, "")
			continue
		}
		rows = append(rows, bodyLine(w, style.Render(ansi.Truncate(l, max(0, w-len(bodyIndent)), "…"))))
	}
	if hidden > 0 {
		more := bodyLine(w, sty.Subtle.Render(fmt.Sprintf("… (%s hidden)", plural(hidden, "line"))))
		if tail {
			rows = append([]string{more}, rows...)
		} else {
			rows = append(rows, more)
		}
	}
	return strings.Join(rows, "\n")
}

func (t *toolItem) spinning() bool { return t.result == nil && !t.halted }
func (t *toolItem) advance()       { t.anim.Advance() }

// checkItem is one check run by the judge, shaped like a tool call:
// "× check go test ./... exit 1 · 3.2s" and the tail of its output.
type checkItem struct {
	sty     *styles.Styles
	name    string
	command string
	fin     *event.CheckFinished
	anim    *anim.Anim
	halted  bool
}

func (c *checkItem) Render(width int) string {
	w := contentWidth(width)
	sty := c.sty
	f := c.fin

	var icon string
	switch {
	case f != nil && f.Status != event.Passed:
		icon = sty.Run.IconError.Render(styles.IconError)
	case f != nil:
		icon = sty.Run.IconSuccess.Render(styles.IconSuccess)
	case c.halted:
		icon = sty.Run.IconIdle.Render(styles.IconPending)
	default:
		icon = sty.Run.IconPending.Render(styles.IconPending)
	}
	prefix := icon + " " + sty.Run.Name.Render("check") + " "

	var suffix string
	switch {
	case c.spinning():
		suffix = " " + c.anim.Render()
	case f == nil:
		suffix = " " + sty.Run.Waiting.Render("Canceled.")
	default:
		var meta []string
		if f.ExitCode > 0 {
			meta = append(meta, fmt.Sprintf("exit %d", f.ExitCode))
		}
		if e := elapsed(f.Elapsed); e != "" {
			meta = append(meta, e)
		}
		if len(meta) > 0 {
			suffix = " " + sty.Subtle.Render(strings.Join(meta, " "+styles.Dot+" "))
		}
	}

	label := firstLine(c.name)
	if cmd := firstLine(c.command); cmd != "" && cmd != label {
		label += " " + styles.Dot + " " + cmd
	}
	room := w - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	header := prefix + sty.Muted.Render(ansi.Truncate(label, max(0, room), "…")) + suffix
	header = ansi.Truncate(header, w, "…")

	if f == nil {
		return indent(header)
	}
	out := header
	if f.Error != "" {
		out += "\n\n" + bodyIndent + tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, f.Error, w-len(bodyIndent))
	}
	if body := outputBody(sty, f.Output, f.Status != event.Passed, true, w); body != "" {
		out += "\n\n" + body
	}
	return indent(out)
}

func (c *checkItem) spinning() bool { return c.fin == nil && !c.halted }
func (c *checkItem) advance()       { c.anim.Advance() }

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(cleanText(s)), "\n")
	return line
}

// agentFooterItem closes one agent attempt:
// "◇ claude-sonnet · 7 turns · $0.12 · 41s ────".
type agentFooterItem struct {
	sty   *styles.Styles
	agent string
	model string
	fin   event.AgentFinished
}

func (a *agentFooterItem) Render(width int) string {
	w := contentWidth(width)
	sty := a.sty
	f := a.fin

	icon := sty.Run.IconSuccess
	if f.Status == event.Failed {
		icon = sty.Run.IconError
	}
	who := a.model
	if who == "" {
		who = a.agent
	}
	var parts []string
	if f.Status == event.Failed {
		parts = append(parts, "failed")
	}
	parts = append(parts, who, plural(f.Turns, "turn"), money(f.CostUSD))
	if e := roundElapsed(f.Elapsed); e != "" {
		parts = append(parts, e)
	}
	line := footerRule(sty, icon, styles.IconInfo, strings.Join(parts, " "+styles.Dot+" "), w)
	if f.Error != "" {
		line += "\n\n" + tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, f.Error, w)
	}
	return indent(line)
}

// judgeRunItem heads a judge scenario run inside a session, shaped like a
// tool call so it reads as something the loop did.
type judgeRunItem struct {
	sty *styles.Styles
	run *Run
}

func (j *judgeRunItem) Render(width int) string {
	w := contentWidth(width)
	sty := j.sty
	r := j.run

	icon := sty.Run.IconPending.Render(styles.IconPending)
	if res := r.Result(); res != nil {
		switch {
		case r.Canceled():
			icon = sty.Run.IconIdle.Render(styles.IconPending)
		case res.Status == event.Passed:
			icon = sty.Run.IconSuccess.Render(styles.IconSuccess)
		default:
			icon = sty.Run.IconError.Render(styles.IconError)
		}
	}
	prefix := icon + " " + sty.Run.Name.Render("Judge") + " "
	params := []event.Param{{Key: "network", Value: r.Network}}
	line := prefix + paramList(sty, r.Name(), params, max(0, w-lipgloss.Width(prefix)))
	return indent(ansi.Truncate(line, w, "…"))
}

// judgeSummaryItem sums up the judges after an attempt and lists what they
// found, since that is what the repair prompt is built from.
type judgeSummaryItem struct {
	sty *styles.Styles
	fin event.JudgeFinished

	// checks that ran in the attempt, the rest of fin's counts are scenarios
	checks, checksFailed int
}

func (j *judgeSummaryItem) Render(width int) string {
	w := contentWidth(width)
	sty := j.sty
	f := j.fin
	total := f.Passed + f.Failed

	var head string
	switch {
	case j.checks > 0:
		// "judge failed · 1 check, 0 scenarios" counts what failed, a pass
		// counts what ran
		scenarios := max(0, total-j.checks)
		checks := j.checks
		word, icon := "judge passed", sty.Run.IconSuccess.Render(styles.IconSuccess)
		if f.Status != event.Passed {
			scenarios = max(0, f.Failed-j.checksFailed)
			checks = j.checksFailed
			word, icon = "judge failed", sty.Run.IconError.Render(styles.IconError)
		}
		counts := plural(checks, "check") + ", " + plural(scenarios, "scenario")
		head = icon + " " + sty.Base.Render(word) + " " + sty.Subtle.Render(styles.Dot+" "+counts)
	case f.Status == event.Passed:
		head = sty.Run.IconSuccess.Render(styles.IconSuccess) + " " + sty.Base.Render("judge passed")
		if total > 0 {
			head += " " + sty.Subtle.Render(fmt.Sprintf("%d/%d scenarios", f.Passed, total))
		}
	default:
		head = sty.Run.IconError.Render(styles.IconError) + " " + sty.Base.Render("judge failed")
		var parts []string
		if len(f.Findings) > 0 {
			parts = append(parts, plural(len(f.Findings), "finding"))
		}
		if total > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d scenarios", f.Passed, total))
		}
		if len(parts) > 0 {
			head += " " + sty.Subtle.Render(styles.Dot+" "+strings.Join(parts, " "+styles.Dot+" "))
		}
	}
	lines := []string{ansi.Truncate(head, w, "…")}

	hang := len(bodyIndent) + 2
	for _, finding := range f.Findings {
		// check findings carry the command output, which the check item
		// above already shows
		wrapped := ansi.Wrap(firstLine(finding), max(1, w-hang), "")
		for i, l := range strings.Split(wrapped, "\n") {
			lead := strings.Repeat(" ", hang)
			if i == 0 {
				lead = bodyIndent + sty.Subtle.Render(styles.Dot) + " "
			}
			lines = append(lines, lead+sty.Muted.Render(l))
		}
	}
	return indent(strings.Join(lines, "\n"))
}

// loopFooterItem closes the whole session:
// "✓ done in 2 attempts · $0.31 · 2m10s ────".
type loopFooterItem struct {
	sty      *styles.Styles
	result   event.LoopFinished
	canceled bool
}

func (l *loopFooterItem) Render(width int) string {
	w := contentWidth(width)
	sty := l.sty
	r := l.result

	var parts []string
	icon, glyph := sty.Run.IconSuccess, styles.IconSuccess
	switch {
	case l.canceled:
		icon, glyph = sty.Subtle, styles.IconInfo
		parts = append(parts, "canceled")
		if r.Attempts > 0 {
			parts = append(parts, fmt.Sprintf("attempt %d", r.Attempts))
		}
	case r.Status == event.Passed:
		parts = append(parts, "done in "+plural(r.Attempts, "attempt"))
	default:
		icon, glyph = sty.Run.IconError, styles.IconError
		if r.Attempts > 0 {
			parts = append(parts, "gave up after "+plural(r.Attempts, "attempt"))
		} else {
			parts = append(parts, "failed")
		}
	}
	if r.CostUSD > 0 {
		parts = append(parts, money(r.CostUSD))
	}
	if e := roundElapsed(r.Elapsed); e != "" {
		parts = append(parts, e)
	}
	line := footerRule(sty, icon, glyph, strings.Join(parts, " "+styles.Dot+" "), w)
	if r.Error != "" {
		line += "\n\n" + tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, r.Error, w)
	}
	return indent(line)
}
