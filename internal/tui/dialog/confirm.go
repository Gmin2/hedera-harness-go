package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const (
	QuitID    = "quit"
	TestnetID = "testnet"
)

// Confirm is a yes/no question with two buttons. No is selected by default
// so a stray enter never does the risky thing.
type Confirm struct {
	id       string
	sty      *styles.Styles
	heading  string
	lines    []string
	hint     []string
	yes, no  string
	onYes    Action
	yesFocus bool
	warn     bool
	// quitKey makes ctrl+c answer yes, so pressing it twice quits.
	quitKey bool
}

// NewQuit asks before leaving. running mentions that the scenario in flight
// gets canceled.
func NewQuit(sty *styles.Styles, running bool) *Confirm {
	c := &Confirm{
		id:      QuitID,
		sty:     sty,
		lines:   []string{"Quit hedera harness?"},
		hint:    []string{"Press ctrl+c again to quit."},
		yes:     "Quit",
		no:      "Stay",
		onYes:   ActionQuit{},
		quitKey: true,
	}
	if running {
		c.lines = append(c.lines, "The running scenario will be canceled.")
	}
	return c
}

// NewTestnetConfirm warns before a run that costs real testnet hbar.
func NewTestnetConfirm(sty *styles.Styles, scenario, path, operator string) *Confirm {
	payer := operator
	if payer == "" {
		payer = "the configured operator"
	}
	return &Confirm{
		id:      TestnetID,
		sty:     sty,
		heading: "Run on testnet?",
		lines: []string{
			"Running " + scenario + " on testnet",
			"spends testnet hbar from " + payer + ".",
		},
		yes:   "Run it",
		no:    "Cancel",
		onYes: ActionRun{Path: path, Confirmed: true},
		warn:  true,
	}
}

func (c *Confirm) ID() string { return c.id }

func (c *Confirm) HandleMsg(msg tea.Msg) Action {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case c.quitKey && key.Matches(k, keyQuitNow):
		return c.onYes
	case key.Matches(k, keyClose, keyNo):
		return ActionClose{}
	case key.Matches(k, keyYes):
		return c.onYes
	case key.Matches(k, keySwitch):
		c.yesFocus = !c.yesFocus
	case key.Matches(k, keyEnter), k.String() == "space":
		if c.yesFocus {
			return c.onYes
		}
		return ActionClose{}
	}
	return nil
}

func (c *Confirm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	sty := c.sty
	var rows []string
	if c.heading != "" {
		rows = append(rows, sty.Dialog.WarnTitle.Render(c.heading), "")
	}
	for _, l := range c.lines {
		rows = append(rows, sty.Dialog.Text.Render(l))
	}
	rows = append(rows, "", c.buttons())
	if len(c.hint) > 0 {
		rows = append(rows, "")
		for _, h := range c.hint {
			rows = append(rows, sty.Dialog.Hint.Render(h))
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Center, rows...)

	frame := sty.Dialog.Frame
	if c.warn {
		frame = sty.Dialog.WarnView
	}
	if lipgloss.Width(content)+frame.GetHorizontalFrameSize() > area.Dx() {
		frame = frame.Padding(1, 0)
	}
	return drawCenter(scr, area, frame.Render(content), nil)
}

func (c *Confirm) buttons() string {
	yes, no := c.sty.Button.Blurred, c.sty.Button.Focused
	if c.yesFocus {
		yes, no = no, yes
	}
	return yes.Render(c.yes) + "  " + no.Render(c.no)
}
