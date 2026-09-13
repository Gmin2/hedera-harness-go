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
	session *run.Session
	view    *run.View
	runGen  int
	cancel  context.CancelFunc
	last    *lastRun
	ticking bool

	judges []string // scenario paths the agent loop is checked with
	checks []Check  // shell commands the agent loop is checked with

	wallet        Wallet
	walletChoice  WalletChoice // kept so a refresh can reconnect an imported key
	walletDlg     *dialog.Wallet
	walletGen     int
	walletPending bool // a ConnectWallet call is in flight
	walletLoading bool // reconnecting after a network switch
	walletSpin    *anim.Anim
}

// activity is what the run list shows: a scenario run or an agent session.
type activity interface {
	Items() []run.Item
	Apply(event.Event)
	End(error)
	Spinning() bool
	Advance()
	Done() bool
}

func (m *Model) active() activity {
	switch {
	case m.session != nil:
		return m.session
	case m.run != nil:
		return m.run
	}
	return nil
}

func newModel(ctx context.Context, opts Options) *Model {
	if len(opts.Networks) == 0 {
		opts.Networks = []string{"mock", "local", "testnet"}
	}
	if opts.Network == "" {
		opts.Network = opts.Networks[0]
	}
	if opts.AgentName == "" {
		opts.AgentName = "agent"
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
		wallet:   opts.Wallet,
	}
	m.walletChoice.Kind = opts.Wallet.Kind
	m.walletSpin = anim.New(anim.Settings{
		Size:   4,
		From:   styles.Primary,
		To:     styles.Secondary,
		Seed:   "wallet pill",
		Static: opts.NoAnim,
	})
	for _, path := range opts.Judges {
		if path != "" && !slices.Contains(m.judges, path) {
			m.judges = append(m.judges, path)
		}
	}
	for _, c := range opts.Checks {
		if c.Run = strings.TrimSpace(c.Run); c.Run == "" {
			continue
		}
		if c.Name = strings.TrimSpace(c.Name); c.Name == "" {
			c.Name = c.Run
		}
		m.checks = append(m.checks, c)
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
		if msg.gen == m.runGen && m.active() != nil {
			m.applyEvent(msg.ev)
		}

	case event.Event:
		// Events sent straight to the program belong to the current run.
		if m.active() != nil {
			m.applyEvent(msg)
		}

	case launchDoneMsg:
		if msg.gen == m.runGen && m.active() != nil {
			m.finishRun(msg.err)
			cmds = append(cmds, m.refreshWallet())
		}

	case walletsMsg:
		if msg.dlg == m.walletDlg && m.walletDialogOpen() {
			options := make([]dialog.WalletOption, len(msg.wallets))
			for i, w := range msg.wallets {
				options[i] = dialog.WalletOption(w)
			}
			m.walletDlg.SetWallets(options, msg.err)
		}

	case walletMsg:
		cmds = append(cmds, m.handleWalletMsg(msg))

	case animTickMsg:
		m.ticking = false
		if a := m.active(); a != nil && a.Spinning() {
			a.Advance()
		}
		if m.walletLoading {
			m.walletSpin.Advance()
		}
		if m.walletDialogOpen() && m.walletDlg.Busy() {
			m.walletDlg.Advance()
		}

	case tea.MouseClickMsg:
		if !m.dialog.HasDialogs() && msg.Button == tea.MouseLeft && m.walletHit(msg.X, msg.Y) {
			cmds = append(cmds, m.openWallet())
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
	if m.animating() && !m.ticking && !m.opts.NoAnim {
		m.ticking = true
		cmds = append(cmds, tea.Tick(anim.FrameInterval(), func(time.Time) tea.Msg { return animTickMsg{} }))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) animating() bool {
	if a := m.active(); a != nil && a.Spinning() {
		return true
	}
	return m.walletLoading || m.walletDialogOpen() && m.walletDlg.Busy()
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
		return m.openScenarios(false)
	case key.Matches(msg, k.Network):
		m.openNetworks()
		return nil
	case key.Matches(msg, k.Wallet):
		return m.openWallet()
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

// submit runs one line typed into the editor. A line starting with a slash
// is always a command. Otherwise it is a command when it starts with a
// command word and has at most one argument, and anything else is a prompt
// for the agent, when there is one.
func (m *Model) submit(line string) tea.Cmd {
	if line == "" {
		return nil
	}
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)

	if name, ok := strings.CutPrefix(cmd, "/"); ok {
		// check is slash only, "check the payout works" is a prompt
		if name == "check" {
			return m.checkCommand(arg)
		}
		if !slices.Contains(commandWords, name) {
			return m.showToast(toastError, fmt.Sprintf("unknown command %q, try /run, /judge, /check, /network, /wallet, /new, /clear or /quit", cmd))
		}
		return m.command(name, arg)
	}
	if m.opts.Agent != nil && !m.isCommand(cmd, arg) {
		return m.requestAgent(line)
	}
	return m.command(cmd, arg)
}

var commandWords = []string{"run", "judge", "network", "wallet", "clear", "new", "quit", "exit"}

func (m *Model) command(cmd, arg string) tea.Cmd {
	switch cmd {
	case "run":
		if arg == "" {
			return m.openScenarios(false)
		}
		path, ok := m.resolveScenario(arg)
		if !ok {
			return m.showToast(toastError, fmt.Sprintf("no scenario named %q", arg))
		}
		return m.requestRun(path, false)
	case "judge":
		switch arg {
		case "":
			return m.openScenarios(true)
		case "off", "clear":
			return m.clearJudges()
		}
		path, ok := m.resolveScenario(arg)
		if !ok {
			return m.showToast(toastError, fmt.Sprintf("no scenario named %q", arg))
		}
		return m.addJudge(path)
	case "network":
		if arg == "" {
			m.openNetworks()
			return nil
		}
		return m.setNetwork(arg)
	case "wallet":
		return m.openWallet()
	case "clear":
		return m.clearRun()
	case "new":
		return m.newConversation()
	case "quit", "exit":
		if m.running() {
			m.dialog.Open(dialog.NewQuit(m.sty, true))
			return nil
		}
		return tea.Quit
	}
	return m.showToast(toastError, fmt.Sprintf("unknown command %q, try run, network, clear or quit", cmd))
}

// isCommand decides whether a line is a command or a prompt for the agent.
// Scenario names can have spaces, so run and judge also count when the rest
// of the line names a known scenario.
func (m *Model) isCommand(cmd, arg string) bool {
	switch cmd {
	case "run", "judge":
		if !strings.ContainsAny(arg, " \t\n") {
			return true
		}
		_, ok := m.resolveScenario(arg)
		return ok
	case "network":
		return !strings.ContainsAny(arg, " \t\n")
	case "wallet", "clear", "new", "quit", "exit":
		return arg == ""
	}
	return false
}

// resolveScenario matches by name, then path, then file name. Anything that
// looks like a yaml path is passed through for the runner to judge.
func (m *Model) resolveScenario(arg string) (string, bool) {
	for _, s := range m.opts.Scenarios {
		if strings.EqualFold(s.Name, arg) || s.Path == arg {
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
	a := m.active()
	return a != nil && !a.Done()
}

// busy is the warning for starting something while the agent or a scenario
// is still going.
func (m *Model) busy() tea.Cmd {
	if m.session != nil {
		return m.showToast(toastWarn, m.opts.AgentName+" is still working, esc to cancel")
	}
	return m.showToast(toastWarn, "a scenario is already running, esc cancels it")
}

func (m *Model) requestRun(path string, confirmed bool) tea.Cmd {
	if m.running() {
		return m.busy()
	}
	if m.network == "testnet" && !confirmed {
		m.dialog.Open(dialog.NewTestnetConfirm(m.sty, m.scenarioName(path), path, m.payer()))
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
	m.session = nil
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

// requestAgent sends prompt to the agent. It continues the current
// conversation when there is one, otherwise it starts a new list.
func (m *Model) requestAgent(prompt string) tea.Cmd {
	if m.running() {
		// hand the text back so it is not lost
		m.editor.SetValue(prompt)
		return m.busy()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.runGen++
	gen := m.runGen

	if m.session == nil {
		m.session = run.NewSession(m.sty, m.opts.AgentName, m.opts.NoAnim)
		m.run = nil
		m.view = run.NewView()
	}
	req := AgentRequest{
		Prompt:    prompt,
		Network:   m.network,
		SessionID: m.session.SessionID(),
		Judges:    slices.Clone(m.judges),
		Checks:    slices.Clone(m.checks),
	}
	m.session.Send(prompt, m.network, m.selectedJudges(), m.selectedChecks())
	m.view.ScrollToBottom()
	m.state = stateRun
	m.detailsOpen = false
	m.setFocus(focusEditor)
	m.updatePlaceholder()

	agent, send := m.opts.Agent, m.send
	return func() tea.Msg {
		err := agent(ctx, req, func(ev event.Event) {
			if send != nil {
				send(eventMsg{gen: gen, ev: ev})
			}
		})
		return launchDoneMsg{gen: gen, err: err}
	}
}

func (m *Model) addJudge(path string) tea.Cmd {
	name := m.scenarioName(path)
	if slices.Contains(m.judges, path) {
		return m.showToast(toastWarn, name+" is already a judge")
	}
	m.judges = append(m.judges, path)
	m.updatePlaceholder()
	msg := name + " added as a judge"
	if m.running() {
		msg += ", used from the next prompt"
	}
	return m.showToast(toastSuccess, msg)
}

func (m *Model) clearJudges() tea.Cmd {
	if len(m.judges) == 0 {
		return m.showToast(toastWarn, "no judges set")
	}
	m.judges = nil
	m.updatePlaceholder()
	return m.showToast(toastSuccess, "judges cleared")
}

// checkCommand handles /check <command> and /check off.
func (m *Model) checkCommand(arg string) tea.Cmd {
	switch arg {
	case "":
		return m.showToast(toastWarn, "add a check with /check <shell command>, /check off clears them")
	case "off", "clear":
		return m.clearChecks()
	}
	return m.addCheck(Check{Name: arg, Run: arg})
}

func (m *Model) addCheck(c Check) tea.Cmd {
	for _, have := range m.checks {
		if have.Run == c.Run {
			return m.showToast(toastWarn, c.Name+" is already a check")
		}
	}
	m.checks = append(m.checks, c)
	m.updatePlaceholder()
	msg := c.Name + " added as a check"
	if m.running() {
		msg += ", used from the next prompt"
	}
	return m.showToast(toastSuccess, msg)
}

func (m *Model) clearChecks() tea.Cmd {
	if len(m.checks) == 0 {
		return m.showToast(toastWarn, "no checks set")
	}
	m.checks = nil
	m.updatePlaceholder()
	return m.showToast(toastSuccess, "checks cleared")
}

func (m *Model) checkNames() []string {
	names := make([]string, len(m.checks))
	for i, c := range m.checks {
		names[i] = c.Name
	}
	return names
}

func (m *Model) judgeNames() []string {
	names := make([]string, len(m.judges))
	for i, path := range m.judges {
		names[i] = m.scenarioName(path)
	}
	return names
}

func (m *Model) applyEvent(ev event.Event) {
	if e, ok := ev.(event.RunStarted); ok && e.Operator != "" {
		m.operator = e.Operator
	}
	a := m.active()
	a.Apply(ev)
	if a.Done() {
		m.updatePlaceholder()
	}
}

func (m *Model) finishRun(err error) {
	m.active().End(err)
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
	if m.session != nil {
		return m.showToast(toastWarn, "canceling "+m.session.Agent)
	}
	return m.showToast(toastWarn, "canceling "+m.run.Name())
}

func (m *Model) clearRun() tea.Cmd {
	if m.running() {
		return m.busy()
	}
	switch {
	case m.session != nil:
		if runs := m.session.Runs(); len(runs) > 0 {
			m.rememberRun(runs[len(runs)-1])
		}
	case m.run != nil:
		m.rememberRun(m.run)
	}
	m.run = nil
	m.session = nil
	m.view = run.NewView()
	m.state = stateLanding
	m.detailsOpen = false
	m.setFocus(focusEditor)
	m.updatePlaceholder()
	return nil
}

// newConversation forgets the agent session so the next prompt starts over.
func (m *Model) newConversation() tea.Cmd {
	if m.running() {
		return m.busy()
	}
	m.clearRun()
	return m.showToast(toastSuccess, "new conversation")
}

func (m *Model) rememberRun(r *run.Run) {
	if r == nil || r.Result() == nil {
		return
	}
	res := r.Result()
	_, _, steps := r.StepCounts()
	_, _, asserts := r.AssertionCounts()
	m.last = &lastRun{
		name:    r.Name(),
		network: r.Network,
		result:  *res,
		steps:   steps,
		asserts: asserts,
	}
}

func (m *Model) setNetwork(name string) tea.Cmd {
	if !slices.Contains(m.opts.Networks, name) {
		return m.showToast(toastError, fmt.Sprintf("unknown network %q, pick one of %s", name, strings.Join(m.opts.Networks, ", ")))
	}
	var reconnect tea.Cmd
	if name != m.network {
		m.network = name
		reconnect = m.reconnectWallet()
	}
	if m.running() {
		return tea.Batch(reconnect, m.showToast(toastSuccess, "the next run uses "+name))
	}
	return tea.Batch(reconnect, m.showToast(toastSuccess, "network set to "+name))
}

func (m *Model) updatePlaceholder() {
	agent := m.opts.AgentName
	switch {
	case m.session != nil && m.running():
		m.editor.Placeholder = agent + " is working... esc to cancel"
	case m.session != nil:
		m.editor.Placeholder = "Reply, or /new to start over"
	case m.running():
		m.editor.Placeholder = "Running " + m.scenarioName(m.run.Path) + "... esc to cancel"
	case m.run != nil:
		m.editor.Placeholder = "Run finished. Try run <scenario> again, or clear"
	case m.opts.Agent != nil && len(m.judges) == 0 && len(m.checks) == 0:
		m.editor.Placeholder = "Ready. Ask " + agent + " for a change, add a judge <scenario> or run <scenario>"
	case m.opts.Agent != nil:
		m.editor.Placeholder = "Ready. Ask " + agent + " for a change, or run <scenario>"
	default:
		m.editor.Placeholder = "Ready. Try run <scenario>, network <name> or ctrl+p"
	}
}

func (m *Model) openCommands() {
	items := []dialog.Item{
		{Title: "Run scenario", Info: "ctrl+r", Value: "run"},
		{Title: "Switch network", Info: "ctrl+n", Value: "network"},
		{Title: "Connect wallet", Info: "ctrl+w", Value: "wallet"},
		{Title: "Use scenario as judge", Value: "judge"},
	}
	if len(m.judges) > 0 {
		items = append(items, dialog.Item{Title: "Clear judges", Value: "clear-judges"})
	}
	if len(m.checks) > 0 {
		items = append(items, dialog.Item{Title: "Clear checks", Info: "/check off", Value: "clear-checks"})
	}
	if m.opts.Agent != nil {
		items = append(items, dialog.Item{Title: "New conversation", Info: "/new", Value: "new"})
	}
	if m.running() {
		title := "Cancel run"
		if m.session != nil {
			title = "Cancel " + m.opts.AgentName
		}
		items = append(items, dialog.Item{Title: title, Info: "esc", Value: "cancel"})
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

// openScenarios opens the scenario picker. With judge set the pick is added
// to the judges instead of run.
func (m *Model) openScenarios(judge bool) tea.Cmd {
	if len(m.opts.Scenarios) == 0 {
		return m.showToast(toastWarn, "no scenarios found")
	}
	items := make([]dialog.Item, len(m.opts.Scenarios))
	for i, s := range m.opts.Scenarios {
		info := fmt.Sprintf("%d steps %s %d checks", s.Steps, styles.Dot, s.Assertions)
		if judge && slices.Contains(m.judges, s.Path) {
			info = "judge " + styles.Dot + " " + info
		}
		items[i] = dialog.Item{Title: s.Name, Info: info, Value: s.Path}
	}
	opts := dialog.PickerOpts{
		ID:          scenariosID,
		Title:       "Run Scenario",
		Items:       items,
		Filter:      true,
		Placeholder: "Filter scenarios",
		OnPick:      func(it dialog.Item) dialog.Action { return dialog.ActionRun{Path: it.Value} },
	}
	if judge {
		opts.Title = "Use Scenario as Judge"
		opts.OnPick = func(it dialog.Item) dialog.Action { return dialog.ActionJudge{Path: it.Value} }
	}
	m.dialog.Open(dialog.NewPicker(m.sty, opts))
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
	case dialog.ActionJudge:
		m.dialog.CloseFront()
		return m.addJudge(a.Path)
	case dialog.ActionNetwork:
		m.dialog.CloseFront()
		return m.setNetwork(a.Name)
	case dialog.ActionConnectWallet:
		if m.opts.ConnectWallet == nil {
			m.walletDlg.SetError("connecting wallets is unavailable")
			return nil
		}
		return m.connectWallet(WalletChoice{Kind: a.Kind, AccountID: a.AccountID, Key: a.Key}, fromDialog)
	case dialog.ActionCommand:
		m.dialog.CloseFront()
		switch a.ID {
		case "run":
			return m.openScenarios(false)
		case "judge":
			return m.openScenarios(true)
		case "clear-judges":
			return m.clearJudges()
		case "clear-checks":
			return m.clearChecks()
		case "network":
			m.openNetworks()
		case "wallet":
			return m.openWallet()
		case "cancel":
			if m.running() {
				return m.cancelRunWithToast()
			}
		case "clear":
			return m.clearRun()
		case "new":
			return m.newConversation()
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
