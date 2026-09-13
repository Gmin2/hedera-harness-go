package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Quit      key.Binding
	Commands  key.Binding
	Scenarios key.Binding
	Network   key.Binding
	Help      key.Binding
	Details   key.Binding
	Cancel    key.Binding
	Tab       key.Binding
	Submit    key.Binding
	Newline   key.Binding

	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	Top      key.Binding
	Bottom   key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Commands:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
		Scenarios: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "scenarios")),
		Network:   key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "network")),
		Help:      key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "more")),
		Details:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "details")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel run")),
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus run")),
		Submit:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Newline:   key.NewBinding(key.WithKeys("ctrl+j", "shift+enter"), key.WithHelp("ctrl+j", "newline")),

		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "scroll")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup/pgdn", "page")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f", "space"), key.WithHelp("pgdn", "page down")),
		HalfUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u/d", "half page")),
		HalfDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half page down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "top/bottom")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
	}
}

// ShortHelp implements help.KeyMap, picking hints for the current screen
// and focus.
func (m *Model) ShortHelp() []key.Binding {
	k := m.keys
	if m.state == stateLanding {
		submit := k.Submit
		submit.SetHelp("enter", "run command")
		if m.opts.Agent != nil {
			submit.SetHelp("enter", "send")
		}
		return []key.Binding{submit, k.Scenarios, k.Network, k.Commands, k.Quit, k.Help}
	}

	var binds []key.Binding
	if m.running() {
		cancel := k.Cancel
		if m.session != nil {
			cancel.SetHelp("esc", "cancel "+m.opts.AgentName)
		}
		binds = append(binds, cancel)
	}
	tab := k.Tab
	if m.focus == focusMain {
		tab.SetHelp("tab", "focus editor")
		binds = append(binds, k.Up, k.Top, tab)
	} else {
		binds = append(binds, tab, k.Scenarios)
	}
	if m.compact {
		details := k.Details
		if m.detailsOpen {
			details.SetHelp("ctrl+d", "close details")
		}
		binds = append(binds, details)
	}
	return append(binds, k.Commands, k.Quit, k.Help)
}

// FullHelp implements help.KeyMap.
func (m *Model) FullHelp() [][]key.Binding {
	k := m.keys
	return [][]key.Binding{
		{k.Commands, k.Scenarios, k.Network, k.Quit},
		{k.Cancel, k.Tab, k.Details, k.Help},
		{k.Up, k.PageUp, k.HalfUp, k.Top},
		{k.Submit, k.Newline},
	}
}
