package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/demo"
	"github.com/Gmin2/hedera-harness-go/internal/tui/dialog"
)

const testKey = "302e020100300506032b657004220420abcdef0123456789"

var (
	mockWallet   = Wallet{Kind: "default", Label: "Mock operator", Detail: "in process", Account: "0.0.2", KeyType: "ed25519", Balance: "10000.00 ℏ", Status: "ok"}
	soloWallet   = Wallet{Kind: "default", Label: "Solo genesis", Detail: "from the solo deployment", Account: "0.0.2", KeyType: "ed25519", Status: "error", Note: "local network not reachable"}
	burnerWallet = Wallet{Kind: "burner", Label: "Burner wallet", Detail: "fresh per run, funded 20 ℏ", KeyType: "ecdsa", Status: "ok"}
)

func portalWallet(balance float64) Wallet {
	w := Wallet{Kind: "default", Label: "Portal account", Detail: "from .env", Account: "0.0.6123456", KeyType: "ecdsa", Balance: fmt.Sprintf("%.2f ℏ", balance), Status: "ok"}
	if balance < 5 {
		w.Status, w.Note = "warn", fmt.Sprintf("balance low %.1f ℏ", balance)
	}
	return w
}

// fakeWallets stands in for the cli: it records every connect and spends
// a little testnet hbar on each one so refreshes show up.
type fakeWallets struct {
	block    chan struct{}
	fail     error
	calls    []WalletChoice
	networks []string
	balance  float64
}

func (f *fakeWallets) list(ctx context.Context, network string) ([]Wallet, error) {
	if f.block != nil {
		<-f.block
	}
	switch network {
	case "testnet":
		return []Wallet{portalWallet(f.balance), burnerWallet}, nil
	case "local":
		return []Wallet{soloWallet, burnerWallet}, nil
	}
	return []Wallet{mockWallet, burnerWallet}, nil
}

func (f *fakeWallets) connect(ctx context.Context, network string, c WalletChoice) (Wallet, error) {
	f.calls = append(f.calls, c)
	f.networks = append(f.networks, network)
	if f.fail != nil {
		return Wallet{}, f.fail
	}
	switch {
	case c.Kind == "burner":
		return burnerWallet, nil
	case c.Kind == "import":
		return Wallet{Kind: "import", Label: "Imported key", Account: c.AccountID, KeyType: "ed25519", Balance: "42.00 ℏ", Status: "ok"}, nil
	case network == "testnet":
		w := portalWallet(f.balance)
		f.balance -= 0.5
		return w, nil
	}
	return mockWallet, nil
}

func (f *fakeWallets) last() WalletChoice { return f.calls[len(f.calls)-1] }

func newWalletModel(w, h int, network string, connected Wallet) (*Model, *fakeWallets) {
	f := &fakeWallets{balance: 97.44}
	opts := testOptions()
	opts.Network = network
	opts.Wallet = connected
	opts.Wallets = f.list
	opts.ConnectWallet = f.connect
	m := newModel(context.Background(), opts)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, f
}

// send updates the model and settles the command it returns.
func send(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	_, cmd := m.Update(msg)
	settle(m, cmd)
}

// settle runs cmd like the program would and feeds the wallet messages it
// produces back into the model. Timers, like the toast expiry, are dropped.
func settle(m *Model, cmd tea.Cmd) {
	for _, msg := range runCmd(cmd) {
		switch msg.(type) {
		case walletsMsg, walletMsg:
			_, next := m.Update(msg)
			settle(m, next)
		}
	}
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range batch {
				msgs = append(msgs, runCmd(c)...)
			}
			return msgs
		}
		return []tea.Msg{msg}
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func plainView(m *Model) string {
	return ansi.Strip(m.View().Content)
}

func TestWalletDialog(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		m, f := newWalletModel(120, 32, "mock", mockWallet)
		f.block = make(chan struct{})
		defer close(f.block)
		press(m, "ctrl+w")
		if !m.dialog.Contains(dialog.WalletID) {
			t.Fatal("ctrl+w did not open the wallet dialog")
		}
		if !strings.Contains(plainView(m), "checking wallets…") {
			t.Fatal("no loading row while the wallets are checked")
		}
		requireGolden(t, m)
	})

	t.Run("listed", func(t *testing.T) {
		m, f := newWalletModel(120, 32, "testnet", Wallet{})
		f.balance = 3.2
		m.wallet = portalWallet(3.2)
		m.walletChoice.Kind = "default"
		send(t, m, keyMsg("ctrl+w"))
		out := plainView(m)
		for _, want := range []string{"Portal account connected", "0.0.6123456", "3.20 ℏ", "balance low 3.2 ℏ", "Burner wallet", "Import key…", "enter connect"} {
			if !strings.Contains(out, want) {
				t.Errorf("dialog is missing %q", want)
			}
		}
		requireGolden(t, m)
	})

	t.Run("import", func(t *testing.T) {
		m, f := newWalletModel(120, 32, "mock", mockWallet)
		typeText(m, "/wallet")
		send(t, m, keyMsg("enter"))
		press(m, "up", "enter")
		typeText(m, "0.0.1234")
		press(m, "enter")
		if len(f.calls) != 0 || !strings.Contains(plainView(m), "enter an account id and a key") {
			t.Fatal("a missing key should be refused before connecting")
		}
		typeText(m, " "+testKey)
		out := plainView(m)
		if strings.Contains(out, testKey[6:12]) || !strings.Contains(out, "0.0.1234 302e02•••") {
			t.Fatalf("key is not masked:\n%s", out)
		}
		requireGolden(t, m)

		send(t, m, keyMsg("enter"))
		if got := f.last(); got.Kind != "import" || got.AccountID != "0.0.1234" || got.Key != testKey {
			t.Fatalf("connect got %+v", got)
		}
		if m.dialog.HasDialogs() || m.wallet.Account != "0.0.1234" {
			t.Fatal("import did not connect")
		}
		if strings.Contains(plainView(m), testKey[6:12]) {
			t.Fatal("key leaked into the ui")
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		m := newTestModel(120, 32)
		press(m, "ctrl+p")
		typeText(m, "connect wallet")
		press(m, "enter")
		if !m.dialog.Contains(dialog.WalletID) || !strings.Contains(plainView(m), "wallets are unavailable") {
			t.Fatal("the palette entry should open the dialog with a hint")
		}
	})
}

func TestWalletConnect(t *testing.T) {
	m, f := newWalletModel(120, 32, "testnet", Wallet{})
	if !strings.Contains(plainView(m), "operator 0.0.2") {
		t.Fatal("without a wallet the landing page should fall back to the operator")
	}
	send(t, m, keyMsg("ctrl+w"))
	send(t, m, keyMsg("enter"))

	if m.dialog.HasDialogs() {
		t.Fatal("dialog should close once connected")
	}
	if f.last().Kind != "default" || f.networks[0] != "testnet" {
		t.Fatalf("connect got %+v on %v", f.calls, f.networks)
	}
	if m.toast.kind != toastSuccess || m.toast.text != "connected 0.0.6123456" {
		t.Fatalf("toast = %+v", m.toast)
	}
	t.Run("landing", func(t *testing.T) {
		if !strings.Contains(plainView(m), "● 0.0.6123456 · 97.44 ℏ · Portal account") {
			t.Fatal("pill missing from the landing page")
		}
		requireGolden(t, m)
	})

	m.Update(clearToastMsg{id: m.toast.id})
	t.Run("sidebar", func(t *testing.T) {
		startRun(t, m, "testnet", 11)
		out := plainView(m)
		for _, want := range []string{"Wallet ─", "Portal account", "0.0.6123456 · ecdsa"} {
			if !strings.Contains(out, want) {
				t.Errorf("sidebar is missing %q", want)
			}
		}
		requireGolden(t, m)
	})
}

func TestWalletBurner(t *testing.T) {
	m, _ := newWalletModel(120, 32, "mock", mockWallet)
	send(t, m, keyMsg("ctrl+w"))
	send(t, m, keyMsg("down"))
	send(t, m, keyMsg("enter"))
	if m.toast.text != "burner wallet ready" {
		t.Fatalf("toast = %+v", m.toast)
	}
	if !strings.Contains(plainView(m), "● burner · fresh per run") {
		t.Fatal("burner pill missing")
	}
}

func TestWalletConnectError(t *testing.T) {
	m, f := newWalletModel(120, 32, "mock", mockWallet)
	f.fail = errors.New("INVALID_SIGNATURE: key does not match account")
	send(t, m, keyMsg("ctrl+w"))
	send(t, m, keyMsg("enter"))
	if !m.dialog.Contains(dialog.WalletID) {
		t.Fatal("a failed connect should keep the dialog open")
	}
	if m.wallet != mockWallet {
		t.Fatalf("wallet changed to %+v", m.wallet)
	}
	if !strings.Contains(plainView(m), "key does not match account") {
		t.Fatal("error not shown under the row")
	}
	requireGolden(t, m)

	f.fail = nil
	send(t, m, keyMsg("enter"))
	if m.dialog.HasDialogs() {
		t.Fatal("retry should connect")
	}
}

func TestWalletNetworkSwitch(t *testing.T) {
	m, f := newWalletModel(120, 32, "mock", mockWallet)
	typeText(m, "network testnet")
	_, cmd := m.Update(keyMsg("enter"))
	if !m.walletLoading || !strings.Contains(plainView(m), "connecting wallet…") {
		t.Fatal("pill should show it is reconnecting")
	}
	settle(m, cmd)
	if m.walletLoading || m.wallet.Account != "0.0.6123456" {
		t.Fatalf("wallet after switch = %+v", m.wallet)
	}
	if f.last().Kind != "default" || f.networks[len(f.networks)-1] != "testnet" {
		t.Fatalf("reconnect got %+v on %v", f.calls, f.networks)
	}

	m.walletChoice = WalletChoice{Kind: "import", AccountID: "0.0.1234", Key: testKey}
	typeText(m, "network mock")
	send(t, m, keyMsg("enter"))
	if got := f.last(); got.Kind != "default" || got.Key != "" {
		t.Fatalf("an imported key should fall back to default, got %+v", got)
	}

	f.fail = errors.New("local network not reachable")
	typeText(m, "network local")
	send(t, m, keyMsg("enter"))
	if m.wallet.Status != "error" || m.toast.kind != toastError {
		t.Fatalf("failed reconnect: wallet %+v toast %+v", m.wallet, m.toast)
	}
}

func TestWalletRefreshAfterRun(t *testing.T) {
	m, f := newWalletModel(120, 32, "testnet", portalWallet(97.44))
	f.balance = 96.1
	events := demo.Events(scenarioPath, "testnet")
	startRun(t, m, "testnet", len(events)-1)
	m.Update(eventMsg{gen: m.runGen, ev: events[len(events)-1]})
	send(t, m, launchDoneMsg{gen: m.runGen})

	if len(f.calls) != 1 || f.last().Kind != "default" {
		t.Fatalf("refresh calls = %+v", f.calls)
	}
	if m.wallet.Balance != "96.10 ℏ" || m.toast.kind != toastNone {
		t.Fatalf("refresh should update quietly, wallet %+v toast %+v", m.wallet, m.toast)
	}
	if !strings.Contains(plainView(m), "96.10 ℏ") {
		t.Fatal("sidebar did not pick up the new balance")
	}
}

func TestWalletAgentSidebar(t *testing.T) {
	m, _ := newWalletModel(120, 32, "mock", mockWallet)
	m.opts.AgentName = "claude"
	m.opts.Agent = func(context.Context, AgentRequest, event.Sink) error { return nil }
	startAgent(t, m, nil)
	if out := plainView(m); !strings.Contains(out, "Wallet ─") || !strings.Contains(out, "Mock operator") {
		t.Fatalf("agent sidebar has no wallet section:\n%s", out)
	}
}

func TestWalletCompact(t *testing.T) {
	m, _ := newWalletModel(90, 28, "testnet", portalWallet(97.44))
	startRun(t, m, "testnet", 20)
	if !m.compact {
		t.Fatal("90x28 should be compact")
	}
	if head := strings.Split(plainView(m), "\n")[1]; !strings.Contains(head, "● 0.0.61…3456 97.4 ℏ") {
		t.Fatalf("header = %q", head)
	}
	requireGolden(t, m)
}

func TestWalletPillClick(t *testing.T) {
	m, _ := newWalletModel(120, 32, "mock", mockWallet)
	lines := strings.Split(plainView(m), "\n")
	for y, l := range lines {
		if i := strings.Index(l, "0.0.2 ·"); i >= 0 {
			x := ansi.StringWidth(l[:i])
			send(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if !m.dialog.Contains(dialog.WalletID) {
				t.Fatalf("click at %d,%d did not open the dialog", x, y)
			}
			return
		}
	}
	t.Fatal("pill not found")
}

func TestShortWallet(t *testing.T) {
	for in, want := range map[string]string{"0.0.6123456": "0.0.61…3456", "0.0.2": "0.0.2"} {
		if got := shortAccount(in); got != want {
			t.Errorf("shortAccount(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"97.44 ℏ": "97.4 ℏ", "": "", "lots": "lots"} {
		if got := shortBalance(in); got != want {
			t.Errorf("shortBalance(%q) = %q", in, got)
		}
	}
	if got := dialog.MaskKey("0.0.1 " + testKey); got != "0.0.1 302e02•••" {
		t.Errorf("MaskKey = %q", got)
	}
}
