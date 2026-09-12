package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const toastTTL = 5 * time.Second

type toastKind uint8

const (
	toastNone toastKind = iota
	toastSuccess
	toastWarn
	toastError
)

// toast is a short message drawn over the help row.
type toast struct {
	id   int
	kind toastKind
	text string
}

type clearToastMsg struct{ id int }

// showToast sets the toast and returns the command that clears it after
// the ttl. A newer toast keeps an older timer from clearing it.
func (m *Model) showToast(kind toastKind, text string) tea.Cmd {
	m.toast = toast{id: m.toast.id + 1, kind: kind, text: text}
	id := m.toast.id
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return clearToastMsg{id: id} })
}

func (m *Model) drawStatus(scr uv.Screen, area uv.Rectangle) {
	m.help.SetWidth(max(0, area.Dx()-m.sty.Help.View.GetHorizontalFrameSize()))
	uv.NewStyledString(m.sty.Help.View.Render(m.help.View(m))).Draw(scr, area)

	if m.toast.kind == toastNone {
		return
	}
	st := m.sty.Status
	tag, msg := st.SuccessTag, st.SuccessMsg
	switch m.toast.kind {
	case toastWarn:
		tag, msg = st.WarnTag, st.WarnMsg
	case toastError:
		tag, msg = st.ErrorTag, st.ErrorMsg
	}

	ind := tag.String()
	avail := max(0, area.Dx()-lipgloss.Width(ind)-msg.GetHorizontalPadding())
	text := ansi.Truncate(strings.ReplaceAll(m.toast.text, "\n", " "), avail, "…")
	text += strings.Repeat(" ", max(0, avail-lipgloss.Width(text)))

	row := area
	row.Max.Y = row.Min.Y + 1
	uv.NewStyledString(ind+msg.Render(text)).Draw(scr, row)
}
