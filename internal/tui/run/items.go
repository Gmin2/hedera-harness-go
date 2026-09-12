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

const (
	maxItemWidth = 120
	gutter       = 2
	bodyKeyWidth = 10
)

// Item is one entry in the run list.
type Item interface {
	Render(width int) string
}

type spinner interface {
	spinning() bool
	advance()
}

// contentWidth is the width items lay themselves out in, after the gutter.
func contentWidth(width int) int {
	return max(0, min(width-gutter, maxItemWidth))
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = strings.Repeat(" ", gutter) + l
		}
	}
	return strings.Join(lines, "\n")
}

func newSpinner(sty *styles.Styles, static bool, seed, label string) *anim.Anim {
	return anim.New(anim.Settings{
		Size:       15,
		Label:      label,
		From:       styles.Primary,
		To:         styles.Secondary,
		LabelColor: styles.FgMostSubtle,
		Seed:       seed,
		Static:     static,
	})
}

func elapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// paramList renders "main (k=v, k=v)", dropping the pairs when they would
// leave the line too cramped, and truncating to width.
func paramList(sty *styles.Styles, main string, params []event.Param, width int) string {
	var pairs []string
	for _, p := range params {
		if p.Value != "" {
			pairs = append(pairs, p.Key+"="+p.Value)
		}
	}
	out := main
	if len(pairs) > 0 {
		joined := "(" + strings.Join(pairs, ", ") + ")"
		if out == "" {
			out = joined
		} else if width < 0 || width-lipgloss.Width(joined)-1 >= min(30, lipgloss.Width(out)) {
			out += " " + joined
		}
	}
	if width >= 0 {
		out = ansi.Truncate(out, width, "…")
	}
	return sty.Run.Params.Render(out)
}

// bodyLine lays pre styled segments on the shaded body background and pads
// the rest of the row so the block reads as one panel.
func bodyLine(sty *styles.Styles, width int, segments ...string) string {
	line := sty.Run.BodyLine.Render(" ") + strings.Join(segments, "")
	if lipgloss.Width(line) > width {
		return ansi.Truncate(line, width, "…")
	}
	return line + sty.Run.BodyLine.Render(strings.Repeat(" ", width-lipgloss.Width(line)))
}

func bodyKV(sty *styles.Styles, width int, key string, value ...string) string {
	k := sty.Run.BodyKey.Render(fmt.Sprintf("%-*s", bodyKeyWidth, key))
	return bodyLine(sty, width, append([]string{k}, value...)...)
}

// link renders text as an osc 8 hyperlink when there is a url.
func link(style lipgloss.Style, url, text string) string {
	if url == "" {
		return style.Render(text)
	}
	return style.Hyperlink(url).Render(text)
}

func shortURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	return strings.TrimPrefix(url, "http://")
}

func tagLine(tag lipgloss.Style, msg lipgloss.Style, text string, width int) string {
	t := tag.String()
	text = strings.ReplaceAll(text, "\n", " ")
	text = ansi.Truncate(text, max(0, width-lipgloss.Width(t)-1), "…")
	return t + " " + msg.Render(text)
}

// commandItem echoes what the user asked for, like a sent prompt.
type commandItem struct {
	sty  *styles.Styles
	text string
}

func (c *commandItem) Render(width int) string {
	w := contentWidth(width) + gutter
	text := ansi.Truncate(c.text, max(0, w-2), "…")
	return c.sty.Run.Command.Render(text)
}

// ruleItem is a section divider inside the run list.
type ruleItem struct {
	sty   *styles.Styles
	title string
}

func (r *ruleItem) Render(width int) string {
	return indent(r.sty.Rule(r.title, contentWidth(width)))
}

// pendingItem is a lone spinner, shown until the runner reports in.
type pendingItem struct {
	sty    *styles.Styles
	name   string
	detail string
	anim   *anim.Anim
}

func (p *pendingItem) Render(width int) string {
	w := contentWidth(width)
	prefix := p.sty.Run.IconPending.Render(styles.IconPending) + " " + p.sty.Run.Name.Render(p.name) + " "
	spin := " " + p.anim.Render()
	room := w - lipgloss.Width(prefix) - lipgloss.Width(spin)
	line := prefix + p.sty.Run.Params.Render(ansi.Truncate(p.detail, max(0, room), "…")) + spin
	return indent(ansi.Truncate(line, w, "…"))
}

func (p *pendingItem) spinning() bool { return true }
func (p *pendingItem) advance()       { p.anim.Advance() }

// actorsItem lists accounts as they become ready, with a spinner row while
// some are still being created.
type actorsItem struct {
	sty    *styles.Styles
	want   int
	actors []event.ActorReady
	done   bool
	anim   *anim.Anim
}

func (a *actorsItem) Render(width int) string {
	w := contentWidth(width)
	nameW := 0
	for _, ac := range a.actors {
		nameW = max(nameW, lipgloss.Width(ac.Name))
	}

	var rows []string
	for _, ac := range a.actors {
		name := a.sty.Run.Name.Render(fmt.Sprintf("%-*s", nameW, ac.Name))
		account := link(a.sty.Base.Foreground(styles.FgSubtle), ac.Link, ac.Account)
		var extra []event.Param
		if ac.KeyType != "" {
			extra = append(extra, event.Param{Key: "key", Value: ac.KeyType})
		}
		if ac.Hbar != "" {
			extra = append(extra, event.Param{Key: "hbar", Value: ac.Hbar})
		}
		head := a.sty.Run.IconSuccess.Render(styles.IconSuccess) + " " + name + " " + account
		rest := w - lipgloss.Width(head) - 1
		row := head
		if rest > 0 && len(extra) > 0 {
			row += " " + paramList(a.sty, "", extra, rest)
		}
		rows = append(rows, ansi.Truncate(row, w, "…"))
	}
	if a.spinning() {
		label := fmt.Sprintf("creating actors %d/%d", len(a.actors), a.want)
		row := a.sty.Run.IconPending.Render(styles.IconPending) + " " + a.sty.Run.Waiting.Render(label) + " " + a.anim.Render()
		rows = append(rows, ansi.Truncate(row, w, "…"))
	}
	if len(rows) == 0 {
		rows = append(rows, a.sty.Run.Waiting.Render("no actors"))
	}
	return indent(strings.Join(rows, "\n"))
}

func (a *actorsItem) spinning() bool { return !a.done && len(a.actors) < a.want }
func (a *actorsItem) advance()       { a.anim.Advance() }

// stepItem renders a transaction step the way a tool call is rendered: a
// status header, then a shaded body with the receipt.
type stepItem struct {
	sty    *styles.Styles
	start  event.StepStarted
	finish *event.StepFinished
	anim   *anim.Anim
	halted bool // the run ended before this step finished
}

func (s *stepItem) ok() bool {
	return s.finish != nil && s.finish.Status == event.Passed
}

func (s *stepItem) failed() bool {
	return s.finish != nil && s.finish.Status == event.Failed
}

func (s *stepItem) Render(width int) string {
	w := contentWidth(width)
	sty := s.sty

	var icon string
	switch {
	case s.ok():
		icon = sty.Run.IconSuccess.Render(styles.IconSuccess)
	case s.failed():
		icon = sty.Run.IconError.Render(styles.IconError)
	case s.finish != nil || s.halted:
		icon = sty.Run.IconIdle.Render(styles.IconPending)
	default:
		icon = sty.Run.IconPending.Render(styles.IconPending)
	}
	prefix := icon + " " + sty.Run.Name.Render(s.start.Op) + " "

	var suffix string
	switch {
	case s.spinning():
		suffix = " " + s.anim.Render()
	case s.halted && s.finish == nil:
		suffix = " " + sty.Run.Waiting.Render("Canceled.")
	case s.finish != nil:
		if e := elapsed(s.finish.Elapsed); e != "" {
			suffix = " " + sty.Run.Elapsed.Render(e)
		}
	}

	// Keep room for the spinner so the params get truncated instead.
	room := w - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	header := prefix + paramList(sty, s.start.Target, s.start.Params, room) + suffix
	header = ansi.Truncate(header, w, "…")

	if s.finish == nil {
		return indent(header)
	}
	return indent(header + "\n\n" + s.body(w))
}

func (s *stepItem) body(w int) string {
	sty := s.sty
	f := s.finish
	var lines []string

	if f.Receipt != "" {
		status := sty.Run.BodyOK
		if f.Status == event.Failed {
			status = sty.Run.BodyBad
		}
		lines = append(lines, bodyKV(sty, w, "status", status.Render(f.Receipt)))
	}
	if f.Expected != "" && (f.Expected != "SUCCESS" || f.Receipt != "SUCCESS") {
		note := sty.Run.BodyBad.Render("  mismatch")
		if f.Expected == f.Receipt {
			note = sty.Run.BodyOK.Render("  matched")
		}
		lines = append(lines, bodyKV(sty, w, "expect", sty.Run.BodyValue.Render(f.Expected), note))
	}
	if f.TxID != "" {
		lines = append(lines, bodyKV(sty, w, "tx", sty.Run.BodyValue.Render(f.TxID)))
	}
	if f.Link != "" {
		lines = append(lines, bodyKV(sty, w, "hashscan", link(sty.Run.Link, f.Link, shortURL(f.Link))))
	}
	for _, e := range f.Entities {
		lines = append(lines, bodyKV(sty, w, e.Key, sty.Run.BodyValue.Render(e.Value)))
	}
	if len(lines) == 0 {
		lines = append(lines, bodyLine(sty, w, sty.Run.BodyNote.Render("no receipt")))
	}

	out := strings.Join(lines, "\n")
	if f.Error != "" {
		out += "\n\n" + tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, f.Error, w)
	}
	return out
}

func (s *stepItem) spinning() bool { return s.finish == nil && !s.halted }
func (s *stepItem) advance()       { s.anim.Advance() }

// assertionItem is a PASS/FAIL row comparing expected against actual. The
// title column is shared across assertions so the values line up.
type assertionItem struct {
	sty    *styles.Styles
	kind   string
	title  string
	finish *event.AssertionFinished
	anim   *anim.Anim
	halted bool
	cols   *columns
}

// columns are widths shared by every assertion row so values line up.
type columns struct {
	title, expected int
}

func (a *assertionItem) label() string {
	if a.title == "" {
		return a.kind
	}
	return a.kind + " " + a.title
}

func (a *assertionItem) Render(width int) string {
	w := contentWidth(width)
	sty := a.sty

	var tag string
	switch {
	case a.finish == nil:
		tag = sty.Run.PendingTag.String()
	case a.finish.Status == event.Passed:
		tag = sty.Run.PassTag.String()
	case a.finish.Status == event.Failed:
		tag = sty.Run.FailTag.String()
	default:
		tag = sty.Run.SkipTag.String()
	}
	tagW := lipgloss.Width(tag)

	label := a.label()
	pad := max(0, a.cols.title-lipgloss.Width(label))
	head := tag + " " + sty.Run.Name.Render(a.kind)
	if a.title != "" {
		head += " " + sty.Base.Foreground(styles.FgSubtle).Render(a.title)
	}
	head += strings.Repeat(" ", pad)

	switch {
	case a.spinning():
		head += "  " + a.anim.Render()
	case a.finish == nil:
		head += "  " + sty.Run.Waiting.Render("Canceled.")
	default:
		actual := sty.Base.Foreground(styles.FgSubtle)
		if a.finish.Status == event.Failed {
			actual = sty.Run.FooterFailed
		}
		if a.finish.Expected != "" || a.finish.Actual != "" {
			expected := fmt.Sprintf("%-*s", a.cols.expected, a.finish.Expected)
			head += "  " + sty.Subtle.Render("expected ") + sty.Base.Foreground(styles.FgSubtle).Render(expected) +
				"  " + sty.Subtle.Render("actual ") + actual.Render(a.finish.Actual)
		}
		if e := elapsed(a.finish.Elapsed); e != "" {
			head += "  " + sty.Run.Elapsed.Render(e)
		}
	}
	head = ansi.Truncate(head, w, "…")

	if a.finish == nil || a.finish.Status == event.Passed {
		return indent(head)
	}

	lines := []string{head}
	pad2 := strings.Repeat(" ", tagW+1)
	if a.finish.Source != "" {
		src := sty.Subtle.Render("source ") + link(sty.Muted.Underline(true), a.finish.Source, shortURL(a.finish.Source))
		if a.finish.Attempts > 1 {
			src += sty.Subtle.Render(fmt.Sprintf(" %s %d attempts", styles.Dot, a.finish.Attempts))
		}
		lines = append(lines, ansi.Truncate(pad2+src, w, "…"))
	}
	if a.finish.Error != "" {
		lines = append(lines, pad2+tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, a.finish.Error, w-tagW-1))
	}
	return indent(strings.Join(lines, "\n"))
}

func (a *assertionItem) spinning() bool { return a.finish == nil && !a.halted }
func (a *assertionItem) advance()       { a.anim.Advance() }

type logItem struct {
	sty *styles.Styles
	log event.Log
}

func (l *logItem) Render(width int) string {
	w := contentWidth(width)
	tag := l.sty.Run.InfoTag
	switch l.log.Level {
	case "warn", "warning":
		tag = l.sty.Run.WarnTag
	case "error":
		tag = l.sty.Run.ErrorTag
	}
	return indent(tagLine(tag, l.sty.Run.TagMessage, l.log.Msg, w))
}

// footerItem closes a run: "◇ mock · 8 steps · 5/6 assertions · 3.2s ────".
type footerItem struct {
	sty      *styles.Styles
	network  string
	result   event.RunFinished
	canceled bool
}

func (f *footerItem) Render(width int) string {
	w := contentWidth(width)
	sty := f.sty
	r := f.result

	icon := sty.Run.IconSuccess
	switch {
	case f.canceled:
		icon = sty.Subtle
	case r.Status != event.Passed:
		icon = sty.Run.IconError
	}

	steps := fmt.Sprintf("%d steps", r.StepsOK+r.StepsFail)
	if r.StepsFail > 0 {
		steps = fmt.Sprintf("%d/%d steps", r.StepsOK, r.StepsOK+r.StepsFail)
	}
	parts := []string{f.network, steps, fmt.Sprintf("%d/%d assertions", r.AssertOK, r.AssertOK+r.AssertFail)}
	if f.canceled {
		parts = append([]string{"canceled"}, parts...)
	}
	if e := elapsed(r.Elapsed); e != "" {
		parts = append(parts, e)
	}
	text := strings.Join(parts, " "+styles.Dot+" ")
	text = ansi.Truncate(text, max(0, w-4), "…")

	line := icon.Render(styles.IconInfo) + " " + sty.Run.FooterText.Render(text)
	if rest := w - lipgloss.Width(line) - 1; rest > 0 {
		line += " " + sty.Section.Line.Render(strings.Repeat(styles.RuleChar, rest))
	}
	if r.Error != "" {
		line += "\n\n" + tagLine(sty.Run.ErrorTag, sty.Run.TagMessage, r.Error, w)
	}
	return indent(line)
}
