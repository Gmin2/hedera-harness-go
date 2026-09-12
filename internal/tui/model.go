package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/anim"
	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
	"github.com/Gmin2/hedera-harness-go/internal/tui/run"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

type uiState uint8

const (
	stateLanding uiState = iota
	stateRun
)

type uiFocus uint8

const (
	focusEditor uiFocus = iota
	focusMain
)

const (
	commandsID  = "commands"
	scenariosID = "scenarios"
	networksID  = "networks"
)

type (
	// eventMsg carries a runner event. gen ties it to the run that sent it
	// so a canceled run cannot write into the next one.
	eventMsg struct {
		gen int
		ev  event.Event
	}
	launchDoneMsg struct {
		gen int
		err error
	}
	animTickMsg struct{}
)

// lastRun is what the landing page remembers after a run is cleared.
type lastRun struct {
	name    string
	network string
	result  event.RunFinished
	steps   int
	asserts int
}

// Model is the one bubbletea model.
type Model struct {
	ctx  context.Context
	opts Options
	sty  *styles.Styles
	keys keyMap
	send func(tea.Msg)

	width, height int
	layout        regions
	state         uiState
	focus         uiFocus
	compact       bool
	detailsOpen   bool

	editor textarea.Model
	help   help.Model
	toast  toast
	dialog *dialog.Overlay

	network  string
	operator string

	run     *run.Run
	view    *run.View
	runGen  int
	cancel  context.CancelFunc
	last    *lastRun
	ticking bool
}

func newModel(ctx context.Context, opts Options) *Model {
	if len(opts.Networks) == 0 {
		opts.Networks = []string{"mock", "local", "testnet"}
	}
	if opts.Network == "" {
		opts.Network = opts.Networks[0]
	}
	m := &Model{
		ctx:      ctx,
		opts:     opts,
		sty:      styles.Default(),
		keys:     defaultKeys(),
		dialog:   &dialog.Overlay{},
		network:  opts.Network,
		operator: opts.Operator,
		view:     run.NewView(),
	}
	m.editor = newEditor(m.sty)
	m.help = help.New()
	m.help.Styles = help.Styles{
		ShortKey:       m.sty.Help.Key,
		ShortDesc:      m.sty.Help.Desc,
		ShortSeparator: m.sty.Help.Separator,
		Ellipsis:       m.sty.Help.Separator,
		FullKey:        m.sty.Help.Key,
		FullDesc:       m.sty.Help.Desc,
		FullSeparator:  m.sty.Help.Separator,
	}
	m.updatePlaceholder()
	return m
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case eventMsg:
		if msg.gen == m.runGen && m.run != nil {
			m.applyEvent(msg.ev)
		}

	case event.Event:
		// Events sent straight to the program belong to the current run.
		if m.run != nil {
			m.applyEvent(msg)
		}

	case launchDoneMsg:
		if msg.gen == m.runGen && m.run != nil {
			m.finishRun(msg.err)
		}

	case animTickMsg:
		m.ticking = false
		if m.run != nil && m.run.Spinning() {
			m.run.Advance()
		}

	case clearToastMsg:
		if msg.id == m.toast.id {
			m.toast = toast{id: m.toast.id}
		}

	case tea.MouseWheelMsg:
		if m.state == stateRun && !m.dialog.HasDialogs() {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.view.ScrollBy(-3)
			case tea.MouseWheelDown:
				m.view.ScrollBy(3)
			}
		}

	case tea.KeyPressMsg:
		cmds = append(cmds, m.handleKey(msg))

	case tea.PasteMsg:
		if m.dialog.HasDialogs() {
			cmds = append(cmds, m.handleAction(m.dialog.Update(msg)))
		} else if m.focus == focusEditor {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			cmds = append(cmds, cmd)
		}

	default:
		if m.dialog.HasDialogs() {
			cmds = append(cmds, m.handleAction(m.dialog.Update(msg)))
		} else {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	m.relayout()
	if m.run != nil && m.run.Spinning() && !m.ticking && !m.opts.NoAnim {
		m.ticking = true
		cmds = append(cmds, tea.Tick(anim.FrameInterval(), func(time.Time) tea.Msg { return animTickMsg{} }))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	if m.dialog.HasDialogs() {
		if key.Matches(msg, k.Quit) && m.dialog.Front().ID() != dialog.QuitID {
			m.dialog.Open(dialog.NewQuit(m.sty, m.running()))
			return nil
		}
		return m.handleAction(m.dialog.Update(msg))
	}

	switch {
	case key.Matches(msg, k.Quit):
		m.dialog.Open(dialog.NewQuit(m.sty, m.running()))
		return nil
	case key.Matches(msg, k.Commands):
		m.openCommands()
		return nil
	case key.Matches(msg, k.Scenarios):
		return m.openScenarios()
	case key.Matches(msg, k.Network):
		m.openNetworks()
		return nil
	case key.Matches(msg, k.Help):
		m.help.ShowAll = !m.help.ShowAll
		return nil
	case key.Matches(msg, k.Details):
		if m.state == stateRun && m.compact {
			m.detailsOpen = !m.detailsOpen
			return nil
		}
	case key.Matches(msg, k.Cancel):
		switch {
		case m.running():
			return m.cancelRunWithToast()
		case m.detailsOpen:
			m.detailsOpen = false
		case m.focus == focusMain:
			m.setFocus(focusEditor)
		}
		return nil
	case key.Matches(msg, k.Tab) && m.state == stateRun:
		if m.focus == focusEditor {
			m.setFocus(focusMain)
		} else {
			m.setFocus(focusEditor)
		}
		return nil
	}

	if m.focus == focusMain && m.state == stateRun {
		m.scroll(msg)
		return nil
	}

	if key.Matches(msg, k.Submit) {
		line := strings.TrimSpace(m.editor.Value())
		m.editor.Reset()
		return m.submit(line)
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return cmd
}

func (m *Model) scroll(msg tea.KeyPressMsg) {
	k, v := m.keys, m.view
	half := max(1, m.layout.main.Dy()/2)
	switch {
	case key.Matches(msg, k.Up):
		v.ScrollBy(-1)
	case key.Matches(msg, k.Down):
		v.ScrollBy(1)
	case key.Matches(msg, k.PageUp):
		v.PageUp()
	case key.Matches(msg, k.PageDown):
		v.PageDown()
	case key.Matches(msg, k.HalfUp):
		v.ScrollBy(-half)
	case key.Matches(msg, k.HalfDown):
		v.ScrollBy(half)
	case key.Matches(msg, k.Top):
		v.ScrollToTop()
	case key.Matches(msg, k.Bottom):
		v.ScrollToBottom()
	}
}

func (m *Model) setFocus(f uiFocus) {
	m.focus = f
	if f == focusEditor {
		m.editor.Focus()
	} else {
		m.editor.Blur()
	}
}

// submit runs one line typed into the editor.
func (m *Model) submit(line string) tea.Cmd {
	if line == "" {
		return nil
	}
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)

	switch cmd {
	case "run":
		if arg == "" {
			return m.openScenarios()
		}
		path, ok := m.resolveScenario(arg)
		if !ok {
			return m.showToast(toastError, fmt.Sprintf("no scenario named %q", arg))
		}
		return m.requestRun(path, false)
	case "network":
		if arg == "" {
			m.openNetworks()
			return nil
		}
		return m.setNetwork(arg)
	case "clear":
		return m.clearRun()
	case "quit", "exit":
		if m.running() {
			m.dialog.Open(dialog.NewQuit(m.sty, true))
			return nil
		}
		return tea.Quit
	}
	return m.showToast(toastError, fmt.Sprintf("unknown command %q, try run, network, clear or quit", cmd))
}

// resolveScenario matches by name, then path, then file name. Anything that
// looks like a yaml path is passed through for the runner to judge.
func (m *Model) resolveScenario(arg string) (string, bool) {
	for _, s := range m.opts.Scenarios {
		if s.Name == arg || s.Path == arg {
			return s.Path, true
		}
	}
	for _, s := range m.opts.Scenarios {
		base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
		if base == arg {
			return s.Path, true
		}
	}
	if ext := filepath.Ext(arg); ext == ".yaml" || ext == ".yml" {
		return arg, true
	}
	return "", false
}

func (m *Model) scenarioName(path string) string {
	for _, s := range m.opts.Scenarios {
		if s.Path == path {
			return s.Name
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func (m *Model) running() bool {
	return m.run != nil && !m.run.Done()
}

func (m *Model) requestRun(path string, confirmed bool) tea.Cmd {
	if m.running() {
		return m.showToast(toastWarn, "a scenario is already running, esc cancels it")
	}
	if m.network == "testnet" && !confirmed {
		m.dialog.Open(dialog.NewTestnetConfirm(m.sty, m.scenarioName(path), path, m.operator))
		return nil
	}
	if m.opts.Launch == nil {
		return m.showToast(toastError, "no runner configured")
	}
	return m.launch(path)
}

func (m *Model) launch(path string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.runGen++
	gen := m.runGen

	m.run = run.New(m.sty, "run "+m.scenarioName(path)+" --network "+m.network, path, m.network, m.opts.NoAnim)
	m.view = run.NewView()
	m.state = stateRun
	m.detailsOpen = false
	m.updatePlaceholder()

	launch, send, network := m.opts.Launch, m.send, m.network
	return func() tea.Msg {
		err := launch(ctx, path, network, func(ev event.Event) {
			if send != nil {
				send(eventMsg{gen: gen, ev: ev})
			}
		})
		return launchDoneMsg{gen: gen, err: err}
	}
}

func (m *Model) applyEvent(ev event.Event) {
	if e, ok := ev.(event.RunStarted); ok && e.Operator != "" {
		m.operator = e.Operator
	}
	m.run.Apply(ev)
	if m.run.Done() {
		m.updatePlaceholder()
	}
}

func (m *Model) finishRun(err error) {
	m.run.End(err)
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.updatePlaceholder()
}

func (m *Model) cancelRun() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Model) cancelRunWithToast() tea.Cmd {
	m.cancelRun()
	return m.showToast(toastWarn, "canceling "+m.run.Name())
}

func (m *Model) clearRun() tea.Cmd {
	if m.running() {
		return m.showToast(toastWarn, "cannot clear while a scenario is running")
	}
	if m.run != nil {
		m.rememberRun()
	}
	m.run = nil
	m.view = run.NewView()
	m.state = stateLanding
	m.detailsOpen = false
	m.setFocus(focusEditor)
	m.updatePlaceholder()
	return nil
}

func (m *Model) rememberRun() {
	res := m.run.Result()
	if res == nil {
		return
	}
	_, _, steps := m.run.StepCounts()
	_, _, asserts := m.run.AssertionCounts()
	m.last = &lastRun{
		name:    m.run.Name(),
		network: m.run.Network,
		result:  *res,
		steps:   steps,
		asserts: asserts,
	}
}

func (m *Model) setNetwork(name string) tea.Cmd {
	if !slices.Contains(m.opts.Networks, name) {
		return m.showToast(toastError, fmt.Sprintf("unknown network %q, pick one of %s", name, strings.Join(m.opts.Networks, ", ")))
	}
	m.network = name
	if m.running() {
		return m.showToast(toastSuccess, "the next run uses "+name)
	}
	return m.showToast(toastSuccess, "network set to "+name)
}

func (m *Model) updatePlaceholder() {
	switch {
	case m.running():
		m.editor.Placeholder = "Running " + m.scenarioName(m.run.Path) + "... esc to cancel"
	case m.run != nil:
		m.editor.Placeholder = "Run finished. Try run <scenario> again, or clear"
	default:
		m.editor.Placeholder = "Ready. Try run <scenario>, network <name> or ctrl+p"
	}
}

func (m *Model) openCommands() {
	items := []dialog.Item{
		{Title: "Run scenario", Info: "ctrl+r", Value: "run"},
		{Title: "Switch network", Info: "ctrl+n", Value: "network"},
	}
	if m.running() {
		items = append(items, dialog.Item{Title: "Cancel run", Info: "esc", Value: "cancel"})
	}
	if m.state == stateRun && !m.running() {
		items = append(items, dialog.Item{Title: "Clear run", Value: "clear"})
	}
	if m.state == stateRun && m.compact {
		items = append(items, dialog.Item{Title: "Toggle details", Info: "ctrl+d", Value: "details"})
	}
	items = append(items,
		dialog.Item{Title: "Toggle help", Info: "ctrl+g", Value: "help"},
		dialog.Item{Title: "Quit", Info: "ctrl+c", Value: "quit"},
	)
	m.dialog.Open(dialog.NewPicker(m.sty, dialog.PickerOpts{
		ID:          commandsID,
		Title:       "Commands",
		Items:       items,
		Filter:      true,
		Placeholder: "Type to filter",
		OnPick:      func(it dialog.Item) dialog.Action { return dialog.ActionCommand{ID: it.Value} },
	}))
}

func (m *Model) openScenarios() tea.Cmd {
	if len(m.opts.Scenarios) == 0 {
		return m.showToast(toastWarn, "no scenarios found")
	}
	items := make([]dialog.Item, len(m.opts.Scenarios))
	for i, s := range m.opts.Scenarios {
		items[i] = dialog.Item{
			Title: s.Name,
			Info:  fmt.Sprintf("%d steps %s %d checks", s.Steps, styles.Dot, s.Assertions),
			Value: s.Path,
		}
	}
	m.dialog.Open(dialog.NewPicker(m.sty, dialog.PickerOpts{
		ID:          scenariosID,
		Title:       "Run Scenario",
		Items:       items,
		Filter:      true,
		Placeholder: "Filter scenarios",
		OnPick:      func(it dialog.Item) dialog.Action { return dialog.ActionRun{Path: it.Value} },
	}))
	return nil
}

func (m *Model) openNetworks() {
	items := make([]dialog.Item, len(m.opts.Networks))
	for i, n := range m.opts.Networks {
		items[i] = dialog.Item{Title: n, Info: networkBlurb(n), Value: n, On: n == m.network}
	}
	m.dialog.Open(dialog.NewPicker(m.sty, dialog.PickerOpts{
		ID:     networksID,
		Title:  "Network",
		Items:  items,
		Radio:  true,
		OnPick: func(it dialog.Item) dialog.Action { return dialog.ActionNetwork{Name: it.Value} },
	}))
}

func networkBlurb(n string) string {
	switch n {
	case "mock":
		return "in process"
	case "local":
		return "solo node"
	case "testnet":
		return "costs hbar"
	case "mainnet":
		return "real money"
	}
	return ""
}

func (m *Model) handleAction(a dialog.Action) tea.Cmd {
	switch a := a.(type) {
	case nil:
		return nil
	case dialog.ActionClose:
		m.dialog.CloseFront()
	case dialog.ActionQuit:
		m.cancelRun()
		return tea.Quit
	case dialog.ActionRun:
		m.dialog.CloseFront()
		return m.requestRun(a.Path, a.Confirmed)
	case dialog.ActionNetwork:
		m.dialog.CloseFront()
		return m.setNetwork(a.Name)
	case dialog.ActionCommand:
		m.dialog.CloseFront()
		switch a.ID {
		case "run":
			return m.openScenarios()
		case "network":
			m.openNetworks()
		case "cancel":
			if m.running() {
				return m.cancelRunWithToast()
			}
		case "clear":
			return m.clearRun()
		case "details":
			m.detailsOpen = !m.detailsOpen
		case "help":
			m.help.ShowAll = !m.help.ShowAll
		case "quit":
			m.dialog.Open(dialog.NewQuit(m.sty, m.running()))
		}
	}
	return nil
}
