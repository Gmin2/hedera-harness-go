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
}

// AgentRequest is one prompt sent to the agent.
type AgentRequest struct {
	Prompt    string
	Judges    []string // scenario paths used to check the work
	Network   string
	SessionID string // empty starts a new conversation, otherwise continue it
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
