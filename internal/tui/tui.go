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
