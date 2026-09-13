// Package tui is the interactive terminal ui for hh.
//
// There is one bubbletea model. Everything else is a plain struct the model
// calls into, and the screen is drawn by painting strings into rectangles of
// an ultraviolet screen buffer, with dialogs painted last.
package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

type Scenario struct {
	Name, Path, Description string
	Steps, Assertions       int
}

// Check is a shell command that judges the agent's work next to the
// scenarios, like a build or a test suite.
type Check struct {
	Name string // short label, defaults to the command
	Run  string // shell command
}

// Wallet describes a wallet option or the connected wallet.
type Wallet struct {
	Kind    string // default, burner, import
	Label   string // "Portal account", "Mock operator", "Solo genesis", "Burner wallet", "Imported key"
	Detail  string // short note: "from .env", "in process", "fresh per run, funded 20 ℏ", ...
	Account string // 0.0.x, empty for burner before a run
	KeyType string // ecdsa, ed25519
	Balance string // "97.44 ℏ", empty when unknown
	Status  string // ok, warn, error
	Note    string // why warn or error, eg "key does not match account"
}

// WalletChoice is what the user picked in the connect wallet dialog.
type WalletChoice struct {
	Kind      string // default, burner, import
	AccountID string // import only
	Key       string // import only, never rendered
}

type Options struct {
	Version   string
	Cwd       string
	Network   string // currently selected: mock | local | testnet
	Networks  []string
	Operator  string // e.g. 0.0.2, may be empty until known
	Scenarios []Scenario
	NoAnim    bool // freeze spinners, fixed placeholders, deterministic output for tests

	// Launch runs one scenario and must stream events into sink. The tui
	// calls it in its own goroutine and forwards events into the program.
	// ctx is canceled when the user cancels the run.
	Launch func(ctx context.Context, scenarioPath, network string, sink event.Sink) error

	// Agent runs one prompt of a conversation (the agent turn plus judge and repair
	// attempts) and streams events into sink. nil means agent mode is unavailable.
	Agent     func(ctx context.Context, req AgentRequest, sink event.Sink) error
	AgentName string // e.g. "claude", shown in the ui

	Judges  []string // judge scenario paths preloaded from hh.yaml
	Checks  []Check  // checks preloaded from hh.yaml
	Project string   // path of the loaded hh.yaml, empty when none

	Wallet Wallet // connected at start, zero value when unknown
	// Wallets lists the options for a network, with live balance and checks. Called when the dialog opens.
	Wallets func(ctx context.Context, network string) ([]Wallet, error)
	// ConnectWallet makes a choice the wallet every run and agent loop uses, and returns it checked.
	ConnectWallet func(ctx context.Context, network string, choice WalletChoice) (Wallet, error)
}

// AgentRequest is one prompt sent to the agent.
type AgentRequest struct {
	Prompt    string
	Network   string
	SessionID string   // empty starts a new conversation, otherwise continue it
	Judges    []string // scenario paths used to check the work
	Checks    []Check  // shell commands run before the judge scenarios
}

// Run starts the tui and blocks until the user quits or ctx is done.
func Run(ctx context.Context, opts Options) error {
	m := newModel(ctx, opts)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.send = p.Send

	_, err := p.Run()
	m.cancelRun()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}
