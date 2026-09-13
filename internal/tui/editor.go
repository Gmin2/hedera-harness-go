package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const (
	editorMinHeight = 3
	editorMaxHeight = 10
	// editorMargin is the blank row above the textarea plus one below.
	editorMargin = 2
)

func newEditor(sty *styles.Styles) textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.DynamicHeight = true
	ta.MinHeight = editorMinHeight
	ta.MaxHeight = editorMaxHeight
	ta.SetVirtualCursor(false)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "shift+enter"))

	state := textarea.StyleState{
		Base:        sty.Base,
		Text:        sty.Editor.Text,
		CursorLine:  sty.Base,
		Placeholder: sty.Editor.Placeholder,
		Prompt:      sty.Editor.PromptFocused,
	}
	blurred := state
	blurred.Text = sty.Editor.TextBlurred
	blurred.Prompt = sty.Editor.PromptBlurred
	ta.SetStyles(textarea.Styles{
		Focused: state,
		Blurred: blurred,
		Cursor:  textarea.CursorStyle{Color: styles.Secondary, Shape: tea.CursorBlock, Blink: true},
	})

	ta.SetPromptFunc(4, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			if info.Focused {
				return "  > "
			}
			return "::: "
		}
		if info.Focused {
			return sty.Editor.PromptFocused.Render("::: ")
		}
		return sty.Editor.PromptBlurred.Render("::: ")
	})
	ta.Focus()
	return ta
}
