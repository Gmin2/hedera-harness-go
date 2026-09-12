package dialog

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

// Item is a row in a picker.
type Item struct {
	Title string
	Info  string // right aligned, dimmed
	Value string
	On    bool // radio pickers mark the current choice
}

// Picker is a titled list with an optional fuzzy filter. It is used for
// the command palette, the scenario picker and the network picker.
type Picker struct {
	id     string
	title  string
	sty    *styles.Styles
	items  []Item
	shown  []fuzzy.Match
	cursor int
	offset int
	filter bool
	radio  bool
	input  textinput.Model
	pick   func(Item) Action
}

type PickerOpts struct {
	ID          string
	Title       string
	Items       []Item
	Filter      bool
	Radio       bool
	Placeholder string
	OnPick      func(Item) Action
}

func NewPicker(sty *styles.Styles, o PickerOpts) *Picker {
	p := &Picker{
		id:     o.ID,
		title:  o.Title,
		sty:    sty,
		items:  o.Items,
		filter: o.Filter,
		radio:  o.Radio,
		pick:   o.OnPick,
	}
	if o.Filter {
		in := textinput.New()
		in.Prompt = "> "
		in.Placeholder = o.Placeholder
		in.SetVirtualCursor(false)
		in.SetStyles(textinput.Styles{
			Focused: textinput.StyleState{
				Text:        sty.Base,
				Placeholder: sty.Dialog.Placeholder,
				Prompt:      sty.Dialog.InputPrompt,
			},
			Blurred: textinput.StyleState{
				Text:        sty.Muted,
				Placeholder: sty.Dialog.Placeholder,
				Prompt:      sty.Muted,
			},
			Cursor: textinput.CursorStyle{Color: styles.Secondary, Shape: tea.CursorBlock, Blink: true},
		})
		in.Focus()
		p.input = in
	}
	for i, it := range p.items {
		if it.On {
			p.cursor = i
		}
	}
	p.refilter()
	return p
}

func (p *Picker) ID() string { return p.id }

func (p *Picker) refilter() {
	query := ""
	if p.filter {
		query = strings.TrimSpace(p.input.Value())
	}
	if query == "" {
		p.shown = make([]fuzzy.Match, len(p.items))
		for i, it := range p.items {
			p.shown[i] = fuzzy.Match{Str: it.Title, Index: i}
		}
	} else {
		titles := make([]string, len(p.items))
		for i, it := range p.items {
			titles[i] = it.Title
		}
		p.shown = fuzzy.Find(query, titles)
		p.cursor = 0
	}
	p.cursor = min(max(0, p.cursor), max(0, len(p.shown)-1))
}

func (p *Picker) move(n int) {
	if len(p.shown) == 0 {
		return
	}
	p.cursor = (p.cursor + n + len(p.shown)) % len(p.shown)
}

func (p *Picker) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyClose):
			return ActionClose{}
		case key.Matches(msg, keyUp):
			p.move(-1)
			return nil
		case key.Matches(msg, keyDown):
			p.move(1)
			return nil
		case key.Matches(msg, keyPageUp):
			p.cursor = max(0, p.cursor-5)
			return nil
		case key.Matches(msg, keyPageDn):
			p.cursor = min(len(p.shown)-1, p.cursor+5)
			return nil
		case key.Matches(msg, keyEnter):
			if len(p.shown) == 0 || p.pick == nil {
				return nil
			}
			return p.pick(p.items[p.shown[p.cursor].Index])
		}
		// Without a filter input j and k are free to move the selection.
		if !p.filter {
			switch msg.String() {
			case "k":
				p.move(-1)
			case "j":
				p.move(1)
			}
			return nil
		}
	}
	if p.filter {
		before := p.input.Value()
		p.input, _ = p.input.Update(msg)
		if p.input.Value() != before {
			p.refilter()
		}
	}
	return nil
}

func (p *Picker) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	sty := p.sty
	frame := sty.Dialog.View.GetHorizontalFrameSize()
	width := max(0, min(maxWidth, area.Dx()-2))
	inner := max(0, width-frame)
	height := min(maxHeight, area.Dy()-2)

	parts := []string{title(sty, p.title, inner-sty.Dialog.Title.GetHorizontalFrameSize())}
	fixed := sty.Dialog.View.GetVerticalFrameSize() + 1 + 2 // title, blank, help
	if p.filter {
		p.input.SetWidth(max(1, inner-sty.Dialog.Input.GetHorizontalFrameSize()-lipgloss.Width(p.input.Prompt)-1))
		parts = append(parts, sty.Dialog.Input.Width(inner).Render(p.input.View()))
		fixed += 2 // the input carries its own blank rows above and below
	} else {
		parts = append(parts, "")
	}

	rows := max(1, min(len(p.shown), height-fixed))
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	p.offset = min(p.offset, max(0, len(p.shown)-rows))

	var list []string
	if len(p.shown) == 0 {
		list = append(list, sty.Dialog.Empty.Width(inner).Render("No matches"))
	}
	for i := p.offset; i < min(len(p.shown), p.offset+rows); i++ {
		list = append(list, p.renderItem(p.shown[i], i == p.cursor, inner))
	}
	parts = append(parts, strings.Join(list, "\n"), "")

	hints := []key.Binding{keyUp, keyEnter, keyClose}
	parts = append(parts, helpLine(sty, hints, inner-sty.Dialog.Help.GetHorizontalFrameSize()))

	view := sty.Dialog.View.Width(width).Render(strings.Join(parts, "\n"))

	var cur *tea.Cursor
	if p.filter {
		if c := p.input.Cursor(); c != nil {
			c.X += sty.Dialog.View.GetBorderLeftSize() + sty.Dialog.Input.GetPaddingLeft()
			c.Y += sty.Dialog.View.GetBorderTopSize() + 1 + sty.Dialog.Input.GetPaddingTop()
			cur = c
		}
	}
	return drawCenter(scr, area, view, cur)
}

func (p *Picker) renderItem(m fuzzy.Match, selected bool, width int) string {
	sty := p.sty
	it := p.items[m.Index]
	style, infoStyle := sty.Dialog.Item, sty.Dialog.Info
	if selected {
		style, infoStyle = sty.Dialog.SelectedItem, sty.Dialog.InfoSelected
	}
	lineW := max(0, width-style.GetHorizontalFrameSize())

	titleText := it.Title
	if p.radio {
		mark := styles.RadioOff
		if it.On {
			mark = styles.RadioOn
		}
		titleText = mark + " " + titleText
	}

	var info string
	if it.Info != "" && lineW > 20 {
		text := ansi.Truncate(it.Info, lineW/2, "…")
		info = infoStyle.Background(style.GetBackground()).Render(" " + text)
	}
	titleText = ansi.Truncate(titleText, max(0, lineW-lipgloss.Width(info)), "…")
	gap := strings.Repeat(" ", max(0, lineW-lipgloss.Width(titleText)-lipgloss.Width(info)))

	return style.Render(underline(titleText, m.MatchedIndexes) + gap + info)
}

// underline marks fuzzy matched bytes with raw underline on/off sequences so
// the row background is left untouched.
func underline(s string, matched []int) string {
	if len(matched) == 0 {
		return s
	}
	hit := make(map[int]bool, len(matched))
	for _, i := range matched {
		hit[i] = true
	}
	var b strings.Builder
	on := false
	for i, r := range s {
		if hit[i] != on {
			on = hit[i]
			if on {
				b.WriteString("\x1b[4m")
			} else {
				b.WriteString("\x1b[24m")
			}
		}
		b.WriteRune(r)
	}
	if on {
		b.WriteString("\x1b[24m")
	}
	return b.String()
}
