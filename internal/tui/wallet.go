package tui

import (
	"image"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
	"github.com/Gmin2/hedera-harness-go/internal/tui/logo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

type walletSource uint8

const (
	fromDialog walletSource = iota
	fromNetwork
	fromRefresh
)

type (
	walletsMsg struct {
		dlg     *dialog.Wallet
		wallets []Wallet
		err     error
	}
	walletMsg struct {
		gen    int
		from   walletSource
		choice WalletChoice
		wallet Wallet
		err    error
	}
)

// openWallet opens the connect wallet dialog and starts checking the
// wallets for the current network.
func (m *Model) openWallet() tea.Cmd {
	d := dialog.NewWallet(m.sty, m.network, dialog.WalletOption(m.wallet), m.opts.NoAnim)
	m.walletDlg = d
	m.dialog.Open(d)
	if m.opts.Wallets == nil {
		d.SetUnavailable()
		return nil
	}
	list, ctx, network := m.opts.Wallets, m.ctx, m.network
	return func() tea.Msg {
		ws, err := list(ctx, network)
		return walletsMsg{dlg: d, wallets: ws, err: err}
	}
}

func (m *Model) walletDialogOpen() bool {
	return m.walletDlg != nil && m.dialog.Contains(dialog.WalletID)
}

// connectWallet calls ConnectWallet off the update loop. Every call bumps
// the generation so only the newest answer lands.
func (m *Model) connectWallet(choice WalletChoice, from walletSource) tea.Cmd {
	connect := m.opts.ConnectWallet
	if connect == nil {
		return nil
	}
	m.walletGen++
	m.walletPending = true
	gen, ctx, network := m.walletGen, m.ctx, m.network
	return func() tea.Msg {
		w, err := connect(ctx, network, choice)
		return walletMsg{gen: gen, from: from, choice: choice, wallet: w, err: err}
	}
}

// reconnectWallet runs after a network switch. An imported key belongs to
// one network, so it falls back to the default wallet.
func (m *Model) reconnectWallet() tea.Cmd {
	if m.opts.ConnectWallet == nil {
		return nil
	}
	choice := WalletChoice{Kind: m.walletChoice.Kind}
	if choice.Kind == "" || choice.Kind == "import" {
		choice.Kind = "default"
	}
	m.walletLoading = true
	return m.connectWallet(choice, fromNetwork)
}

// refreshWallet quietly reconnects the current choice after a run so the
// balance is fresh.
func (m *Model) refreshWallet() tea.Cmd {
	if m.opts.ConnectWallet == nil || m.walletPending {
		return nil
	}
	choice := m.walletChoice
	if choice.Kind == "" {
		choice.Kind = "default"
	}
	if choice.Kind == "import" && choice.Key == "" {
		return nil
	}
	return m.connectWallet(choice, fromRefresh)
}

func (m *Model) handleWalletMsg(msg walletMsg) tea.Cmd {
	if msg.gen != m.walletGen {
		return nil
	}
	m.walletPending = false

	switch msg.from {
	case fromRefresh:
		if msg.err == nil {
			m.wallet = msg.wallet
		}
		return nil

	case fromNetwork:
		m.walletLoading = false
		m.walletChoice = msg.choice
		if msg.err != nil {
			m.wallet = Wallet{Kind: msg.choice.Kind, Label: "not connected", Status: "error", Note: msg.err.Error()}
			return m.showToast(toastError, "wallet not connected on "+m.network+": "+msg.err.Error())
		}
		m.wallet = msg.wallet
		return nil
	}

	if msg.err != nil {
		if m.walletDialogOpen() {
			m.walletDlg.SetError(msg.err.Error())
			return nil
		}
		return m.showToast(toastError, "could not connect wallet: "+msg.err.Error())
	}
	m.wallet = msg.wallet
	m.walletChoice = msg.choice
	if m.walletDialogOpen() {
		m.dialog.Close(dialog.WalletID)
	}
	m.walletDlg = nil
	switch {
	case msg.wallet.Kind == "burner":
		return m.showToast(toastSuccess, "burner wallet ready")
	case msg.wallet.Account != "":
		return m.showToast(toastSuccess, "connected "+msg.wallet.Account)
	}
	return m.showToast(toastSuccess, "connected "+walletLabel(msg.wallet))
}

func (m *Model) hasWallet() bool {
	return m.walletLoading || m.wallet != (Wallet{})
}

func walletLabel(w Wallet) string {
	if w.Label != "" {
		return w.Label
	}
	return w.Kind
}

// payer names who pays for a testnet run, for the confirmation.
func (m *Model) payer() string {
	switch {
	case m.wallet.Account != "":
		return m.wallet.Account
	case m.wallet.Kind == "burner":
		return "a fresh burner wallet"
	}
	return m.operator
}

// walletPill is "● 0.0.6123456 · 97.44 ℏ · Portal account".
func (m *Model) walletPill() string {
	sty := m.sty
	if m.walletLoading {
		return m.walletSpin.Render() + " " + sty.Subtle.Render("connecting wallet…")
	}
	w := m.wallet
	sep := sty.Subtle.Render(" " + styles.Dot + " ")
	var parts []string
	if w.Kind == "burner" && w.Account == "" {
		parts = append(parts, sty.Base.Render("burner"), sty.Muted.Render("fresh per run"))
	} else {
		if w.Account != "" {
			parts = append(parts, sty.Base.Render(w.Account))
		}
		if w.Balance != "" {
			parts = append(parts, sty.Base.Render(w.Balance))
		}
		parts = append(parts, sty.Muted.Render(walletLabel(w)))
	}
	if w.Status == "error" && w.Note != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(styles.Error).Render(w.Note))
	}
	return dialog.WalletDot(w.Status) + " " + strings.Join(parts, sep)
}

// shortWalletPill is "● 0.0.61…3456 97.4 ℏ" for the compact header.
func (m *Model) shortWalletPill() string {
	sty := m.sty
	if m.walletLoading {
		return m.walletSpin.Render() + " " + sty.Header.Detail.Render("connecting…")
	}
	w := m.wallet
	text := walletLabel(w)
	switch {
	case w.Kind == "burner" && w.Account == "":
		text = "burner"
	case w.Account != "":
		text = shortAccount(w.Account)
	}
	if b := shortBalance(w.Balance); b != "" {
		text += " " + b
	}
	return dialog.WalletDot(w.Status) + " " + sty.Header.Detail.Render(text)
}

// walletSection is the sidebar block for the connected wallet.
func (m *Model) walletSection(width int) string {
	sty := m.sty
	lines := []string{sty.Rule("Wallet", width)}
	if m.walletLoading {
		return strings.Join(append(lines, m.walletSpin.Render()+" "+sty.Subtle.Render("connecting…")), "\n")
	}
	w := m.wallet
	label := ansi.Truncate(walletLabel(w), max(0, width-3-lipgloss.Width(w.Balance)), "…")
	pad := max(1, width-2-lipgloss.Width(label)-lipgloss.Width(w.Balance))
	lines = append(lines, dialog.WalletDot(w.Status)+" "+sty.Base.Foreground(styles.FgSubtle).Render(label)+strings.Repeat(" ", pad)+sty.Muted.Render(w.Balance))

	account := w.Account
	if account == "" && w.Kind == "burner" {
		account = "fresh per run"
	}
	var bits []string
	for _, b := range []string{account, w.KeyType} {
		if b != "" {
			bits = append(bits, b)
		}
	}
	if len(bits) > 0 {
		lines = append(lines, "  "+sty.Muted.Render(ansi.Truncate(strings.Join(bits, " "+styles.Dot+" "), max(0, width-2), "…")))
	}
	if w.Note != "" && (w.Status == "warn" || w.Status == "error") {
		c := styles.Warning
		if w.Status == "error" {
			c = styles.Error
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(c).Render(ansi.Truncate(w.Note, max(0, width-2), "…")))
	}
	return strings.Join(lines, "\n")
}

func shortAccount(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[:6] + "…" + id[len(id)-4:]
}

// shortBalance turns "97.44 ℏ" into "97.4 ℏ". Anything it cannot parse is
// left alone.
func shortBalance(b string) string {
	num, unit, _ := strings.Cut(strings.TrimSpace(b), " ")
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return b
	}
	return strings.TrimSpace(strconv.FormatFloat(f, 'f', 1, 64) + " " + unit)
}

// walletHit reports whether a click at x, y lands on the wallet pill on the
// landing page or the wallet section of the sidebar.
func (m *Model) walletHit(x, y int) bool {
	if !m.hasWallet() {
		return false
	}
	p := image.Pt(x, y)
	switch {
	case m.state == stateLanding:
		main := m.layout.main
		// the landing body has one row of padding, then the path, a blank
		// row and the network line
		row := main.Min.Y + 3
		left := main.Min.X + lipgloss.Width(styles.IconInfo+" "+m.sty.NetworkPill(m.network)+" ")
		return p.In(image.Rect(left, row, left+lipgloss.Width(m.walletPill()), row+1))
	case m.state == stateRun && !m.compact:
		side := m.layout.sidebar
		top := side.Min.Y + m.walletSectionRow(side.Dx())
		return p.In(image.Rect(side.Min.X, top, side.Max.X, top+lipgloss.Height(m.walletSection(max(1, side.Dx()-2)))))
	}
	return false
}

// walletSectionRow is the sidebar line the wallet section starts on, below
// the logo, the title and its info line, and the network line.
func (m *Model) walletSectionRow(width int) int {
	w := max(1, width-2)
	title := 1
	if m.session != nil {
		title = lipgloss.Height(m.promptTitle(w, 2))
	}
	return lipgloss.Height(logo.Compact(m.sty, m.opts.Version, w)) + 1 + title + 1 + 1 + 1 + 1
}
