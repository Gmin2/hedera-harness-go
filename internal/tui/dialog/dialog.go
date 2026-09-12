// Package dialog has the overlays drawn on top of the ui: pickers, the
// command palette and confirmations.
package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const (
	maxWidth  = 70
	maxHeight = 20
)

// Action is what a dialog wants the ui to do after handling a message. The
// ui switches on the concrete type.
type Action any

type (
	// ActionClose closes the front dialog.
	ActionClose struct{}
	// ActionQuit quits the program.
	ActionQuit struct{}
	// ActionRun launches a scenario. Confirmed is set once the testnet
	// warning has been accepted.
	ActionRun struct {
		Path      string
		Confirmed bool
	}
	// ActionNetwork selects a network.
	ActionNetwork struct{ Name string }
	// ActionCommand runs a palette command by id.
	ActionCommand struct{ ID string }
)

// Dialog is one overlay.
type Dialog interface {
	ID() string
	HandleMsg(msg tea.Msg) Action
	Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor
}

// Overlay is the stack of open dialogs. Only the front one gets input, all
// of them are drawn in order.
type Overlay struct {
	dialogs []Dialog
}

func (o *Overlay) Open(d Dialog) {
	o.dialogs = append(o.dialogs, d)
}

func (o *Overlay) HasDialogs() bool { return len(o.dialogs) > 0 }

func (o *Overlay) Contains(id string) bool {
	for _, d := range o.dialogs {
		if d.ID() == id {
			return true
		}
	}
	return false
}

func (o *Overlay) Front() Dialog {
	if len(o.dialogs) == 0 {
		return nil
	}
	return o.dialogs[len(o.dialogs)-1]
}

func (o *Overlay) Close(id string) {
	for i, d := range o.dialogs {
		if d.ID() == id {
			o.dialogs = append(o.dialogs[:i], o.dialogs[i+1:]...)
			return
		}
	}
}

func (o *Overlay) CloseFront() {
	if len(o.dialogs) > 0 {
		o.dialogs = o.dialogs[:len(o.dialogs)-1]
	}
}

// Update sends msg to the front dialog.
func (o *Overlay) Update(msg tea.Msg) Action {
	if d := o.Front(); d != nil {
		return d.HandleMsg(msg)
	}
	return nil
}

func (o *Overlay) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	var cur *tea.Cursor
	for _, d := range o.dialogs {
		cur = d.Draw(scr, area)
	}
	return cur
}

// drawCenter paints view in the middle of area and shifts cur, which is
// relative to the view, into screen coordinates.
func drawCenter(scr uv.Screen, area uv.Rectangle, view string, cur *tea.Cursor) *tea.Cursor {
	w, h := lipgloss.Size(view)
	w, h = min(w, area.Dx()), min(h, area.Dy())
	x := area.Min.X + (area.Dx()-w)/2
	y := area.Min.Y + (area.Dy()-h)/2
	uv.NewStyledString(view).Draw(scr, image.Rect(x, y, x+w, y+h))
	if cur != nil {
		cur.X += x
		cur.Y += y
	}
	return cur
}

// title renders "Title ╱╱╱╱" with the gradient rule filling width.
func title(sty *styles.Styles, text string, width int) string {
	if lipgloss.Width(text) >= width {
		return sty.Dialog.Title.Render(ansi.Truncate(text, width, "…"))
	}
	rule := strings.Repeat(styles.Diagonal, max(0, width-lipgloss.Width(text)-1))
	return sty.Dialog.Title.Render(text + " " + styles.Gradient(lipgloss.NewStyle(), rule, sty.Dialog.TitleFrom, sty.Dialog.TitleTo))
}

// helpLine packs key hints into one line, ending with … when some do not fit.
func helpLine(sty *styles.Styles, bindings []key.Binding, width int) string {
	sep := sty.Help.Separator.Render(" • ")
	var b strings.Builder
	used := 0
	for _, kb := range bindings {
		if !kb.Enabled() {
			continue
		}
		seg := sty.Help.Key.Render(kb.Help().Key) + " " + sty.Help.Desc.Render(kb.Help().Desc)
		if used > 0 {
			seg = sep + seg
		}
		w := lipgloss.Width(seg)
		if used+w > width {
			if used+2 <= width {
				b.WriteString(sty.Help.Separator.Render(" …"))
			}
			break
		}
		used += w
		b.WriteString(seg)
	}
	return sty.Dialog.Help.Render(b.String())
}

var (
	keyUp      = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/↓", "choose"))
	keyDown    = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "down"))
	keyEnter   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	keyClose   = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keySwitch  = key.NewBinding(key.WithKeys("left", "right", "tab", "h", "l"), key.WithHelp("←/→", "switch"))
	keyYes     = key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "yes"))
	keyNo      = key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n", "no"))
	keyPageUp  = key.NewBinding(key.WithKeys("pgup"))
	keyPageDn  = key.NewBinding(key.WithKeys("pgdown"))
	keyQuitNow = key.NewBinding(key.WithKeys("ctrl+c"))
)
