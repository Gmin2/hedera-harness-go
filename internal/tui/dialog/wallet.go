package dialog

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/tui/anim"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const WalletID = "wallet"

// WalletOption mirrors tui.Wallet so the two convert into each other.
type WalletOption struct {
	Kind    string
	Label   string
	Detail  string
	Account string
	KeyType string
	Balance string
	Status  string
	Note    string
}

// ActionConnectWallet asks the ui to connect a wallet. Key is only set for
// an imported key.
type ActionConnectWallet struct {
	Kind      string
	AccountID string
	Key       string
}

var (
	accountIDPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

	keyConnect = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect"))
	keyEsc     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))
	keyBack    = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
)

// Wallet is the connect wallet dialog: a list of wallets checked against
// the network, and an input to import a key.
type Wallet struct {
	sty     *styles.Styles
	network string
	current WalletOption

	options     []WalletOption
	loading     bool
	unavailable bool
	listErr     string

	cursor int // len(options) is the import row
	offset int

	connecting int // row being connected, -1 when idle
	rowErr     int
	errText    string

	importing bool
	input     textinput.Model
	spin      *anim.Anim
}

// NewWallet opens the dialog for network. current is the connected wallet,
// used to tag its row.
func NewWallet(sty *styles.Styles, network string, current WalletOption, static bool) *Wallet {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "0.0.1234 302e0201..."
	in.SetVirtualCursor(false)
	in.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{Text: sty.Base, Placeholder: sty.Dialog.Placeholder, Prompt: sty.Dialog.InputPrompt},
		Blurred: textinput.StyleState{Text: sty.Muted, Placeholder: sty.Dialog.Placeholder, Prompt: sty.Muted},
		Cursor:  textinput.CursorStyle{Color: styles.Secondary, Shape: tea.CursorBlock, Blink: true},
	})
	return &Wallet{
		sty:        sty,
		network:    network,
		current:    current,
		loading:    true,
		connecting: -1,
		rowErr:     -1,
		input:      in,
		spin: anim.New(anim.Settings{
			Size:   6,
			From:   styles.Primary,
			To:     styles.Secondary,
			Seed:   "wallet",
			Static: static,
		}),
	}
}

func (w *Wallet) ID() string { return WalletID }

// SetWallets fills the list once the wallets have been checked.
func (w *Wallet) SetWallets(options []WalletOption, err error) {
	w.loading = false
	w.options = options
	w.listErr = ""
	if err != nil {
		w.listErr = err.Error()
	}
	w.cursor = 0
	for i, o := range options {
		if w.isCurrent(o) {
			w.cursor = i
		}
	}
	if w.current.Kind == "import" {
		w.cursor = len(options)
	}
}

// SetUnavailable shows a hint instead of the list, for when the ui was
// started without wallet support.
func (w *Wallet) SetUnavailable() {
	w.loading = false
	w.unavailable = true
}

// SetError ends a connect attempt that failed and shows why under its row.
func (w *Wallet) SetError(text string) {
	w.rowErr = w.connecting
	if w.rowErr < 0 {
		w.rowErr = w.cursor
	}
	w.connecting = -1
	w.errText = text
}

// Busy reports whether the spinner is showing.
func (w *Wallet) Busy() bool { return w.loading || w.connecting >= 0 }

func (w *Wallet) Advance() { w.spin.Advance() }

func (w *Wallet) isCurrent(o WalletOption) bool {
	c := w.current
	if c.Kind == "" || o.Kind != c.Kind {
		return false
	}
	return o.Account == c.Account || c.Kind == "burner"
}

func (w *Wallet) HandleMsg(msg tea.Msg) Action {
	if w.importing {
		return w.handleImport(msg)
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if key.Matches(k, keyClose) {
		return ActionClose{}
	}
	if w.Busy() || w.unavailable {
		return nil
	}
	rows := len(w.options) + 1
	switch {
	case key.Matches(k, keyUp), k.String() == "k":
		w.cursor = (w.cursor - 1 + rows) % rows
	case key.Matches(k, keyDown), k.String() == "j":
		w.cursor = (w.cursor + 1) % rows
	case key.Matches(k, keyEnter):
		w.errText, w.rowErr = "", -1
		if w.cursor == len(w.options) {
			w.importing = true
			w.input.Focus()
			return nil
		}
		w.connecting = w.cursor
		return ActionConnectWallet{Kind: w.options[w.cursor].Kind}
	}
	return nil
}

func (w *Wallet) handleImport(msg tea.Msg) Action {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(k, keyClose):
			if w.connecting < 0 {
				w.importing = false
				w.errText, w.rowErr = "", -1
				w.input.Blur()
			}
			return nil
		case w.connecting >= 0:
			return nil
		case key.Matches(k, keyEnter):
			fields := strings.Fields(w.input.Value())
			switch {
			case len(fields) != 2:
				w.errText = "enter an account id and a key, separated by a space"
				return nil
			case !accountIDPattern.MatchString(fields[0]):
				w.errText = fields[0] + " is not an account id like 0.0.1234"
				return nil
			}
			w.errText = ""
			w.connecting = len(w.options)
			return ActionConnectWallet{Kind: "import", AccountID: fields[0], Key: fields[1]}
		}
	}
	if w.connecting >= 0 {
		return nil
	}
	before := w.input.Value()
	w.input, _ = w.input.Update(msg)
	if w.input.Value() != before {
		w.errText = ""
	}
	return nil
}

// MaskKey hides everything after the first 6 characters of the key in an
// "account key" line.
func MaskKey(line string) string {
	account, rest, ok := strings.Cut(line, " ")
	if !ok {
		return line
	}
	k := strings.TrimLeft(rest, " ")
	lead := rest[:len(rest)-len(k)]
	if r := []rune(k); len(r) > 6 {
		k = string(r[:6]) + "•••"
	}
	return account + " " + lead + k
}

func (w *Wallet) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	sty := w.sty
	width := max(0, min(maxWidth, area.Dx()-2))
	inner := max(0, width-sty.Dialog.View.GetHorizontalFrameSize())
	lineW := max(0, inner-2)
	height := min(maxHeight, area.Dy()-2)

	parts := []string{
		title(sty, "Connect wallet", inner-sty.Dialog.Title.GetHorizontalFrameSize()),
		" " + ansi.Truncate(sty.NetworkPill(w.network)+" "+sty.Subtle.Render("pays and signs every run and agent loop"), lineW, "…"),
		"",
	}

	var body []string
	var cur *tea.Cursor
	hints := []key.Binding{keyUp, keyConnect, keyEsc}
	switch {
	case w.importing:
		body, cur = w.importBody(lineW)
		hints = []key.Binding{keyConnect, keyBack}
		if cur != nil {
			cur.X += sty.Dialog.View.GetBorderLeftSize() + 1
			cur.Y += sty.Dialog.View.GetBorderTopSize() + len(parts)
		}
	case w.unavailable:
		body = []string{sty.Subtle.Render("wallets are unavailable, hh was started without wallet support")}
		hints = []key.Binding{keyEsc}
	case w.loading:
		body = []string{w.spin.Render() + " " + sty.Subtle.Render("checking wallets…")}
		hints = []key.Binding{keyEsc}
	default:
		room := height - sty.Dialog.View.GetVerticalFrameSize() - len(parts) - 2
		body = w.listBody(lineW, room)
	}
	for _, l := range body {
		parts = append(parts, " "+l)
	}
	parts = append(parts, "", helpLine(sty, hints, inner-sty.Dialog.Help.GetHorizontalFrameSize()))

	view := sty.Dialog.View.Width(width).Render(strings.Join(parts, "\n"))
	return drawCenter(scr, area, view, cur)
}

func (w *Wallet) importBody(width int) ([]string, *tea.Cursor) {
	sty := w.sty
	lines := []string{
		sty.Base.Render("Import key"),
		sty.Subtle.Render(ansi.Truncate("account id, a space, then the private key", width, "…")),
		"",
	}

	prompt := sty.Dialog.InputPrompt.Render(w.input.Prompt)
	value := w.input.Value()
	text := sty.Dialog.Placeholder.Render(w.input.Placeholder)
	if value != "" {
		text = sty.Base.Render(MaskKey(value))
	}
	row := ansi.Truncate(prompt+text, width, "…")
	lines = append(lines, row)

	var cur *tea.Cursor
	if w.connecting < 0 {
		pos := min(w.input.Position(), len([]rune(value)))
		x := lipgloss.Width(w.input.Prompt) + lipgloss.Width(MaskKey(string([]rune(value)[:pos])))
		cur = tea.NewCursor(min(x, width), len(lines)-1)
		cur.Color = styles.Secondary
		cur.Blink = true
	}

	switch {
	case w.connecting >= 0:
		lines = append(lines, w.spin.Render()+" "+sty.Subtle.Render("connecting…"))
	case w.errText != "":
		lines = append(lines, w.errLine(width))
	}
	return lines, cur
}

func (w *Wallet) errLine(width int) string {
	return lipgloss.NewStyle().Foreground(styles.Error).Render(ansi.Truncate(styles.IconError+" "+w.errText, width, "…"))
}

// listBody renders the rows, scrolled so the selected one is visible
// within room lines.
func (w *Wallet) listBody(width, room int) []string {
	sty := w.sty
	var blocks [][]string
	if w.listErr != "" {
		blocks = append(blocks, []string{w.colored("error", ansi.Truncate("could not check wallets: "+w.listErr, width, "…"))})
	} else if len(w.options) == 0 {
		blocks = append(blocks, []string{sty.Subtle.Render("no wallets for this network")})
	}
	head := len(blocks)

	accW, balW := 0, 0
	for _, o := range w.options {
		accW = max(accW, lipgloss.Width(o.Account))
		balW = max(balW, lipgloss.Width(o.Balance))
	}
	for i, o := range w.options {
		blocks = append(blocks, w.optionRows(i, o, width, accW, balW))
	}
	blocks = append(blocks, w.importRows(width, accW, balW))

	sel := head + w.cursor
	w.offset = min(w.offset, sel)
	for w.offset < sel && linesIn(blocks[w.offset:sel+1]) > room {
		w.offset++
	}
	var out []string
	for _, b := range blocks[w.offset:] {
		if len(out) > 0 && len(out)+len(b) > room {
			break
		}
		out = append(out, b...)
	}
	return out
}

func linesIn(blocks [][]string) int {
	n := 0
	for _, b := range blocks {
		n += len(b)
	}
	return n
}

func (w *Wallet) optionRows(i int, o WalletOption, width, accW, balW int) []string {
	sty := w.sty
	selected := i == w.cursor

	label := o.Label
	if label == "" {
		label = o.Kind
	}
	right := ""
	if accW > 0 {
		right = sty.Muted.Render(padRight(o.Account, accW))
	}
	if balW > 0 {
		right += "  " + sty.Base.Render(padLeft(o.Balance, balW))
	}
	rows := []string{w.headRow(selected, WalletDot(o.Status), label, w.isCurrent(o), right, width)}

	var bits []string
	for _, b := range []string{o.Detail, o.KeyType} {
		if b != "" {
			bits = append(bits, b)
		}
	}
	detail := strings.Join(bits, " "+styles.Dot+" ")
	switch {
	case w.connecting == i:
		rows = append(rows, "    "+w.spin.Render()+" "+sty.Subtle.Render("connecting…"))
	case detail != "":
		rows = append(rows, "    "+sty.Subtle.Render(ansi.Truncate(detail, max(0, width-4), "…")))
	}
	if selected && o.Note != "" && (o.Status == "warn" || o.Status == "error") {
		rows = append(rows, "    "+w.colored(o.Status, ansi.Truncate(o.Note, max(0, width-4), "…")))
	}
	if w.rowErr == i && w.errText != "" {
		rows = append(rows, "    "+w.errLine(max(0, width-4)))
	}
	return rows
}

func (w *Wallet) importRows(width, accW, balW int) []string {
	sty := w.sty
	i := len(w.options)
	current := w.current.Kind == "import"
	right := ""
	if current && w.current.Account != "" {
		right = sty.Muted.Render(padRight(w.current.Account, accW))
		if balW > 0 {
			right += strings.Repeat(" ", balW+2)
		}
	}
	dot := sty.Subtle.Render("+")
	rows := []string{w.headRow(i == w.cursor, dot, "Import key…", current, right, width)}
	rows = append(rows, "    "+sty.Subtle.Render(ansi.Truncate("use your own account id and private key", max(0, width-4), "…")))
	if w.rowErr == i && w.errText != "" {
		rows = append(rows, "    "+w.errLine(max(0, width-4)))
	}
	return rows
}

// headRow is "▌ ● Label connected      0.0.2  97.44 ℏ". The selection is a
// bar and a brighter label rather than a filled row.
func (w *Wallet) headRow(selected bool, dot, label string, current bool, right string, width int) string {
	sty := w.sty
	bar, labelStyle := " ", sty.Muted
	if selected {
		bar = lipgloss.NewStyle().Foreground(styles.Primary).Render(styles.BorderThick)
		labelStyle = lipgloss.NewStyle().Foreground(styles.FgBase).Bold(true)
	}
	tag := ""
	if current {
		tag = " " + lipgloss.NewStyle().Foreground(styles.Success).Render("connected")
	}
	prefix := bar + " " + dot + " "
	room := max(0, width-lipgloss.Width(prefix)-lipgloss.Width(tag)-lipgloss.Width(right)-2)
	left := prefix + labelStyle.Render(ansi.Truncate(label, room, "…")) + tag
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "…")
}

func (w *Wallet) colored(status, text string) string {
	c := styles.Warning
	if status == "error" {
		c = styles.Error
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// WalletDot is the status dot shared by the dialog and the wallet pill.
func WalletDot(status string) string {
	switch status {
	case "ok":
		return lipgloss.NewStyle().Foreground(styles.Success).Render(styles.IconPending)
	case "warn":
		return lipgloss.NewStyle().Foreground(styles.Warning).Render(styles.IconPending)
	case "error":
		return lipgloss.NewStyle().Foreground(styles.Error).Render(styles.IconPending)
	}
	return lipgloss.NewStyle().Foreground(styles.FgMostSubtle).Render(styles.IconIdle)
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(s))) + s
}
