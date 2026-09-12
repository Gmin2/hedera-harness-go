package run

import (
	"strings"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

// View is a scrolling window over the rendered run items. While following
// it stays pinned to the bottom as new items arrive.
type View struct {
	width, height int
	offset        int
	follow        bool
	lines         []string
}

func NewView() *View {
	return &View{follow: true}
}

func (v *View) SetSize(width, height int) {
	v.width, v.height = width, height
	v.clamp()
}

// Refresh renders items at the current width. Call it after the run changes
// or the size changes.
func (v *View) Refresh(items []Item) {
	v.lines = v.lines[:0]
	for i, it := range items {
		if i > 0 && !stacked(items[i-1], it) {
			v.lines = append(v.lines, "")
		}
		v.lines = append(v.lines, strings.Split(it.Render(v.width), "\n")...)
	}
	v.clamp()
}

// stacked reports whether two neighbours sit without a blank line between
// them. Assertion rows and consecutive log lines read better as a block.
func stacked(prev, next Item) bool {
	switch prev.(type) {
	case *assertionItem:
		_, ok := next.(*assertionItem)
		return ok
	case *logItem:
		_, ok := next.(*logItem)
		return ok
	}
	return false
}

func (v *View) maxOffset() int {
	return max(0, len(v.lines)-v.height)
}

func (v *View) clamp() {
	if v.follow {
		v.offset = v.maxOffset()
		return
	}
	v.offset = min(max(0, v.offset), v.maxOffset())
}

// ScrollBy moves the window by n lines. Scrolling back to the bottom turns
// following on again.
func (v *View) ScrollBy(n int) {
	v.offset = min(max(0, v.offset+n), v.maxOffset())
	v.follow = v.offset >= v.maxOffset()
}

func (v *View) PageUp()   { v.ScrollBy(-max(1, v.height-1)) }
func (v *View) PageDown() { v.ScrollBy(max(1, v.height-1)) }

func (v *View) ScrollToTop() {
	v.offset = 0
	v.follow = v.maxOffset() == 0
}

func (v *View) ScrollToBottom() {
	v.follow = true
	v.clamp()
}

func (v *View) Following() bool { return v.follow }

// Overflows reports whether the content is taller than the window.
func (v *View) Overflows() bool { return len(v.lines) > v.height }

// Render returns the visible lines.
func (v *View) Render() string {
	end := min(len(v.lines), v.offset+v.height)
	if v.offset >= end {
		return ""
	}
	return strings.Join(v.lines[v.offset:end], "\n")
}

// Scrollbar renders a one column scrollbar for the window, or "" when
// everything fits.
func (v *View) Scrollbar(sty *styles.Styles) string {
	total := len(v.lines)
	if total <= v.height || v.height <= 0 {
		return ""
	}
	thumb := max(1, v.height*v.height/total)
	pos := 0
	if m := v.maxOffset(); m > 0 {
		pos = (v.height - thumb) * v.offset / m
	}
	rows := make([]string, v.height)
	for i := range rows {
		if i >= pos && i < pos+thumb {
			rows[i] = sty.Scrollbar.Thumb.Render(styles.ScrollbarThumb)
		} else {
			rows[i] = sty.Scrollbar.Track.Render(styles.ScrollbarTrack)
		}
	}
	return strings.Join(rows, "\n")
}
