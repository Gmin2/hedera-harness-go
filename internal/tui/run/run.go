// Package run turns runner events into the items of the run list and keeps
// the counters the sidebar shows.
package run

import (
	"context"
	"errors"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const maxTitleWidth = 40

// Run is the state of one scenario run as seen by the ui.
type Run struct {
	sty    *styles.Styles
	static bool

	Info    event.RunStarted
	Command string
	Path    string
	Network string

	items   []Item
	pending *pendingItem
	actors  *actorsItem
	steps   map[int]*stepItem
	asserts map[int]*assertionItem
	order   []*stepItem
	checks  []*assertionItem

	stepsRule, assertsRule bool

	cols     columns
	result   *event.RunFinished
	canceled bool
	done     bool
}

// New starts an empty run for the scenario at path. static freezes the
// spinners.
func New(sty *styles.Styles, command, path, network string, static bool) *Run {
	r := newRun(sty, path, network, static)
	r.Command = command
	r.items = append(r.items, &commandItem{sty: sty, text: command})
	r.pending = &pendingItem{sty: sty, name: "launch", detail: path, anim: newSpinner(sty, static, path, "Starting")}
	r.items = append(r.items, r.pending)
	return r
}

// newRun is a run with no items yet. Judge runs inside an agent session
// start this way, since the session draws their header.
func newRun(sty *styles.Styles, path, network string, static bool) *Run {
	return &Run{
		sty:     sty,
		static:  static,
		Path:    path,
		Network: network,
		steps:   make(map[int]*stepItem),
		asserts: make(map[int]*assertionItem),
	}
}

// Apply folds one runner event into the run.
func (r *Run) Apply(ev event.Event) {
	switch ev := ev.(type) {
	case event.RunStarted:
		r.Info = ev
		if ev.Network != "" {
			r.Network = ev.Network
		}
		r.dropPending()
		r.actors = &actorsItem{sty: r.sty, want: ev.Actors, anim: newSpinner(r.sty, r.static, "actors", "")}
		r.items = append(r.items, &ruleItem{sty: r.sty, title: "Actors"}, r.actors)

	case event.ActorReady:
		if r.actors == nil {
			r.dropPending()
			r.actors = &actorsItem{sty: r.sty, anim: newSpinner(r.sty, r.static, "actors", "")}
			r.items = append(r.items, &ruleItem{sty: r.sty, title: "Actors"}, r.actors)
		}
		r.actors.actors = append(r.actors.actors, ev)
		r.actors.want = max(r.actors.want, len(r.actors.actors))

	case event.StepStarted:
		r.step(ev.Index, ev)

	case event.StepFinished:
		s := r.step(ev.Index, event.StepStarted{Index: ev.Index, Op: ev.Op})
		s.finish = &ev

	case event.AssertionStarted:
		r.assertion(ev.Index, ev.Kind, ev.Title)

	case event.AssertionFinished:
		a := r.assertion(ev.Index, ev.Kind, ev.Title)
		a.finish = &ev
		r.cols.expected = min(maxTitleWidth, max(r.cols.expected, lipgloss.Width(ev.Expected)))

	case event.Log:
		r.items = append(r.items, &logItem{sty: r.sty, log: ev})

	case event.RunFinished:
		r.result = &ev
		if ev.RunID != "" && r.Info.RunID == "" {
			r.Info.RunID = ev.RunID
		}
		r.halt()
		r.items = append(r.items, &footerItem{sty: r.sty, network: r.Network, result: ev})
	}
}

// End is called when the launcher returns. If the runner never reported a
// result the run is closed as canceled or failed.
func (r *Run) End(err error) {
	if r.result != nil {
		r.done = true
		return
	}
	res := event.RunFinished{RunID: r.Info.RunID, Status: event.Failed}
	res.StepsOK, res.StepsFail, _ = r.StepCounts()
	res.AssertOK, res.AssertFail, _ = r.AssertionCounts()
	canceled := errors.Is(err, context.Canceled)
	if err != nil && !canceled {
		res.Error = err.Error()
	}
	r.result = &res
	r.canceled = canceled
	r.halt()
	r.items = append(r.items, &footerItem{sty: r.sty, network: r.Network, result: res, canceled: canceled})
}

func (r *Run) halt() {
	r.done = true
	r.dropPending()
	if r.actors != nil {
		r.actors.done = true
	}
	for _, s := range r.order {
		s.halted = true
	}
	for _, a := range r.checks {
		a.halted = true
	}
}

func (r *Run) dropPending() {
	if r.pending == nil {
		return
	}
	for i, it := range r.items {
		if it == Item(r.pending) {
			r.items = append(r.items[:i], r.items[i+1:]...)
			break
		}
	}
	r.pending = nil
}

func (r *Run) step(index int, start event.StepStarted) *stepItem {
	if s, ok := r.steps[index]; ok {
		return s
	}
	r.dropPending()
	if !r.stepsRule {
		r.stepsRule = true
		r.items = append(r.items, &ruleItem{sty: r.sty, title: "Steps"})
	}
	s := &stepItem{
		sty:   r.sty,
		start: start,
		anim:  newSpinner(r.sty, r.static, "step"+strconv.Itoa(index), "Submitting"),
	}
	r.steps[index] = s
	r.order = append(r.order, s)
	r.items = append(r.items, s)
	return s
}

func (r *Run) assertion(index int, kind, title string) *assertionItem {
	if a, ok := r.asserts[index]; ok {
		return a
	}
	r.dropPending()
	if !r.assertsRule {
		r.assertsRule = true
		r.items = append(r.items, &ruleItem{sty: r.sty, title: "Assertions"})
	}
	a := &assertionItem{
		sty:   r.sty,
		kind:  kind,
		title: title,
		cols:  &r.cols,
		anim:  newSpinner(r.sty, r.static, "assert"+strconv.Itoa(index), "Checking"),
	}
	r.cols.title = min(maxTitleWidth, max(r.cols.title, lipgloss.Width(a.label())))
	r.asserts[index] = a
	r.checks = append(r.checks, a)
	r.items = append(r.items, a)
	return a
}

// Items is the run list content in display order.
func (r *Run) Items() []Item { return r.items }

// Spinning reports whether any item is animating.
func (r *Run) Spinning() bool {
	for _, it := range r.items {
		if s, ok := it.(spinner); ok && s.spinning() {
			return true
		}
	}
	return false
}

// Advance steps every running spinner by one frame.
func (r *Run) Advance() {
	for _, it := range r.items {
		if s, ok := it.(spinner); ok && s.spinning() {
			s.advance()
		}
	}
}

// Done reports whether the run has ended, one way or another.
func (r *Run) Done() bool { return r.done }

// Result is the final outcome, nil while running.
func (r *Run) Result() *event.RunFinished { return r.result }

// Canceled reports whether the user stopped the run.
func (r *Run) Canceled() bool { return r.canceled }

// Name is the scenario name, falling back to the path.
func (r *Run) Name() string {
	if r.Info.Scenario != "" {
		return r.Info.Scenario
	}
	return r.Path
}

// Actors returns the accounts created so far and how many are expected.
func (r *Run) Actors() ([]event.ActorReady, int) {
	if r.actors == nil {
		return nil, r.Info.Actors
	}
	return r.actors.actors, max(r.actors.want, r.Info.Actors)
}

// StepCounts returns passed and failed steps and the expected total.
func (r *Run) StepCounts() (ok, failed, total int) {
	for _, s := range r.order {
		switch {
		case s.ok():
			ok++
		case s.failed():
			failed++
		}
	}
	return ok, failed, max(r.Info.Steps, len(r.order))
}

// AssertionCounts returns passed and failed assertions and the expected
// total.
func (r *Run) AssertionCounts() (ok, failed, total int) {
	for _, a := range r.checks {
		if a.finish == nil {
			continue
		}
		switch a.finish.Status {
		case event.Passed:
			ok++
		case event.Failed:
			failed++
		}
	}
	return ok, failed, max(r.Info.Assertions, len(r.checks))
}

// Stage is the phase the run is in, for headers: actors, steps, assertions
// or done.
func (r *Run) Stage() string {
	switch {
	case r.done:
		return "done"
	case len(r.checks) > 0:
		return "assertions"
	case len(r.order) > 0:
		return "steps"
	case r.actors != nil:
		return "actors"
	}
	return "starting"
}
