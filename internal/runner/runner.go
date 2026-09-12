// Package runner executes a scenario against a network target and reports
// progress as events.
package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/assert"
	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

type Options struct {
	Poll assert.Poll
	// MirrorSync is how long to wait for the last transaction to show up on
	// the mirror node before assertions start.
	MirrorSync time.Duration
	// ActorPace spaces account creates, testnet throttles them to 2 per second.
	ActorPace time.Duration
	// KeepGoing runs the remaining steps after an unexpected result.
	KeepGoing bool
}

// Defaults picks sensible timings for a mode.
func Defaults(mode network.Mode) Options {
	switch mode {
	case network.Testnet:
		return Options{Poll: assert.Poll{Timeout: 30 * time.Second, Interval: time.Second}, MirrorSync: 60 * time.Second, ActorPace: 600 * time.Millisecond}
	case network.Local:
		return Options{Poll: assert.Poll{Timeout: 15 * time.Second, Interval: 500 * time.Millisecond}, MirrorSync: 30 * time.Second}
	}
	return Options{Poll: assert.Poll{Timeout: 2 * time.Second, Interval: 50 * time.Millisecond}, MirrorSync: 5 * time.Second}
}

type Report struct {
	RunID    string                    `json:"run_id"`
	Scenario string                    `json:"scenario"`
	Path     string                    `json:"path,omitempty"`
	Network  string                    `json:"network"`
	Operator string                    `json:"operator"`
	Status   event.Status              `json:"status"`
	Actors   []event.ActorReady        `json:"actors"`
	Steps    []event.StepFinished      `json:"steps"`
	Asserts  []event.AssertionFinished `json:"assertions"`
	Elapsed  time.Duration             `json:"elapsed_ns"`
	Error    string                    `json:"error,omitempty"`
}

func (r *Report) counts() (stepsOK, stepsFail, assertOK, assertFail int) {
	for _, s := range r.Steps {
		switch s.Status {
		case event.Passed:
			stepsOK++
		case event.Failed:
			stepsFail++
		}
	}
	for _, a := range r.Asserts {
		switch a.Status {
		case event.Passed:
			assertOK++
		case event.Failed:
			assertFail++
		}
	}
	return
}

// Plan checks a scenario without a network: every op and assertion decodes,
// every name it uses is declared earlier. It returns all problems found.
func Plan(sc *scenario.Scenario) error {
	env := ops.NewEnv(nil)
	var errs []error
	fake := int64(5000)

	for _, spec := range sc.Actors {
		a, _, err := ops.PrepareActor(env, spec)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", spec.Line, err))
			continue
		}
		if spec.ID == "" {
			fake++
			a.ID = hiero.AccountID{Account: uint64(fake)}
		}
		if err := env.AddActor(a); err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", spec.Line, err))
		}
	}

	check := func(st scenario.Step) {
		if isAssertion(st.Op) {
			c, err := assert.Decode(st)
			if err == nil {
				err = c.Resolve(env)
			}
			if err != nil {
				errs = append(errs, lineErr(st, err))
			}
			return
		}
		op, err := ops.Decode(st)
		if err != nil {
			errs = append(errs, err)
			return
		}
		built, err := op.Build(env)
		if err != nil {
			errs = append(errs, lineErr(st, err))
			return
		}
		if err := env.Declare(built.Declares, built.DeclaresKind); err != nil {
			errs = append(errs, lineErr(st, err))
		}
		if c := ops.Settings(op); c.Payer != "" {
			if _, err := env.Actor(c.Payer); err != nil {
				errs = append(errs, lineErr(st, fmt.Errorf("payer: %w", err)))
			}
		}
	}
	for _, st := range sc.Steps {
		check(st)
	}
	for _, st := range sc.Asserts {
		if !isAssertion(st.Op) {
			errs = append(errs, fmt.Errorf("line %d: %s is a step, not an assertion", st.Line, st.Op))
			continue
		}
		check(st)
	}
	return errors.Join(errs...)
}

func lineErr(st scenario.Step, err error) error {
	if strings.HasPrefix(err.Error(), "line ") {
		return err
	}
	return fmt.Errorf("line %d: %s: %w", st.Line, st.Op, err)
}

func isAssertion(op string) bool {
	for _, n := range assert.Names() {
		if n == op {
			return true
		}
	}
	return false
}

type run struct {
	ctx    context.Context
	target *network.Target
	opts   Options
	sink   event.Sink
	env    *ops.Env
	report *Report
	// lastTx is the newest transaction that reached consensus, the mirror
	// must have it before any assertion reads state.
	lastTx   string
	syncedTx string
}

func Run(ctx context.Context, target *network.Target, sc *scenario.Scenario, opts Options, sink event.Sink) *Report {
	if sink == nil {
		sink = func(event.Event) {}
	}
	start := time.Now()
	r := &run{
		ctx:    ctx,
		target: target,
		opts:   opts,
		sink:   sink,
		env:    ops.NewEnv(target),
		report: &Report{
			RunID:    newRunID(),
			Scenario: sc.Name,
			Path:     sc.Path,
			Network:  string(target.Mode),
			Operator: target.OperatorID.String(),
		},
	}

	steps, assertions := Counts(sc)
	sink(event.RunStarted{
		RunID:      r.report.RunID,
		Scenario:   sc.Name,
		Path:       sc.Path,
		Network:    string(target.Mode),
		Operator:   target.OperatorID.String(),
		Actors:     len(sc.Actors),
		Steps:      steps,
		Assertions: assertions,
		At:         start,
	})

	err := r.execute(sc)
	rep := r.report
	rep.Elapsed = time.Since(start)
	stepsOK, stepsFail, assertOK, assertFail := rep.counts()
	rep.Status = event.Passed
	if err != nil || stepsFail > 0 || assertFail > 0 {
		rep.Status = event.Failed
	}
	if err != nil {
		rep.Error = err.Error()
	}
	sink(event.RunFinished{
		RunID:      rep.RunID,
		Status:     rep.Status,
		StepsOK:    stepsOK,
		StepsFail:  stepsFail,
		AssertOK:   assertOK,
		AssertFail: assertFail,
		Elapsed:    rep.Elapsed,
		Error:      rep.Error,
	})
	return rep
}

// Counts reports how many transaction steps and assertions a scenario has,
// counting assertions written inline between steps.
func Counts(sc *scenario.Scenario) (steps, assertions int) {
	inline := countAssertions(sc.Steps)
	return len(sc.Steps) - inline, len(sc.Asserts) + inline
}

func countAssertions(steps []scenario.Step) int {
	n := 0
	for _, st := range steps {
		if isAssertion(st.Op) {
			n++
		}
	}
	return n
}

func (r *run) execute(sc *scenario.Scenario) error {
	if err := Plan(sc); err != nil {
		return fmt.Errorf("scenario is invalid:\n%w", err)
	}

	for i, spec := range sc.Actors {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if i > 0 && r.opts.ActorPace > 0 {
			time.Sleep(r.opts.ActorPace)
		}
		if err := r.createActor(spec); err != nil {
			return err
		}
	}

	stopped := false
	stepIndex, assertIndex := 0, 0
	for _, st := range sc.Steps {
		if isAssertion(st.Op) {
			r.assertion(assertIndex, st, stopped)
			assertIndex++
			continue
		}
		ok := r.step(stepIndex, st, stopped)
		stepIndex++
		if !ok && !r.opts.KeepGoing {
			stopped = true
		}
		if r.ctx.Err() != nil {
			stopped = true
		}
	}
	for _, st := range sc.Asserts {
		r.assertion(assertIndex, st, stopped)
		assertIndex++
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (r *run) createActor(spec scenario.Actor) error {
	a, built, err := ops.PrepareActor(r.env, spec)
	if err != nil {
		return err
	}
	if err := r.env.AddActor(a); err != nil {
		return err
	}
	ready := event.ActorReady{Name: a.Name, KeyType: a.KeyType}
	if built != nil {
		out := ops.Execute(r.env, ops.Common{}, built)
		if out.Err != nil {
			return fmt.Errorf("create actor %s: %w", a.Name, out.Err)
		}
		if out.Status != "SUCCESS" {
			return fmt.Errorf("create actor %s: %s", a.Name, out.Status)
		}
		if _, err := built.Bind(r.env, *out.Receipt); err != nil {
			return err
		}
		r.lastTx = out.TxID
		ready.TxID = out.TxID
		ready.Link = r.target.Link("account", a.ID.String())
	}
	ready.Account = a.ID.String()
	if spec.Hbar != nil {
		ready.Hbar = fmt.Sprintf("%g ℏ", *spec.Hbar)
	} else if spec.ID == "" {
		ready.Hbar = "10 ℏ"
	}
	r.report.Actors = append(r.report.Actors, ready)
	r.sink(ready)
	return nil
}

// step runs one transaction step and reports whether it matched expectations.
func (r *run) step(index int, st scenario.Step, skip bool) bool {
	op, err := ops.Decode(st)
	if err != nil {
		return r.finishStep(event.StepFinished{Index: index, Op: st.Op, Status: event.Failed, Error: err.Error()})
	}
	c := ops.Settings(op)
	expect := "SUCCESS"
	if c.Expect != "" {
		expect = strings.ToUpper(c.Expect)
	}
	started := event.StepStarted{Index: index, Op: st.Op, Target: op.Target(), Params: op.Params()}
	r.sink(started)

	fin := event.StepFinished{Index: index, Op: st.Op, Expected: expect}
	if skip {
		fin.Status = event.Skipped
		return r.finishStep(fin)
	}

	start := time.Now()
	built, err := op.Build(r.env)
	if err != nil {
		fin.Status, fin.Error = event.Failed, err.Error()
		return r.finishStep(fin)
	}
	out := ops.Execute(r.env, c, built)
	fin.Elapsed = time.Since(start)
	fin.TxID = out.TxID
	fin.Receipt = out.Status
	if out.Err != nil {
		fin.Status, fin.Error = event.Failed, out.Err.Error()
		return r.finishStep(fin)
	}
	if out.Receipt != nil {
		r.lastTx = out.TxID
		fin.Link = r.target.Link("transaction", out.TxID)
	}
	if out.Status != expect {
		fin.Status = event.Failed
		fin.Error = fmt.Sprintf("expected %s, got %s", expect, out.Status)
		return r.finishStep(fin)
	}
	fin.Status = event.Passed
	if out.Status == "SUCCESS" && built.Bind != nil && out.Receipt != nil {
		ents, err := built.Bind(r.env, *out.Receipt)
		if err != nil {
			fin.Status, fin.Error = event.Failed, err.Error()
		}
		fin.Entities = ents
	}
	return r.finishStep(fin)
}

func (r *run) finishStep(fin event.StepFinished) bool {
	r.report.Steps = append(r.report.Steps, fin)
	r.sink(fin)
	return fin.Status != event.Failed
}

func (r *run) assertion(index int, st scenario.Step, skip bool) {
	c, err := assert.Decode(st)
	title := st.Op
	if err == nil {
		title = c.Title()
	}
	r.sink(event.AssertionStarted{Index: index, Kind: st.Op, Title: title})

	var res event.AssertionFinished
	switch {
	case err != nil:
		res = event.AssertionFinished{Status: event.Failed, Error: err.Error()}
	case skip:
		res = event.AssertionFinished{Status: event.Skipped, Title: title, Expected: c.Expected()}
	default:
		if err := c.Resolve(r.env); err != nil {
			res = event.AssertionFinished{Status: event.Failed, Title: title, Error: err.Error()}
			break
		}
		r.waitForMirror()
		res = assert.Eval(r.ctx, r.env, r.target.Mirror, c, r.opts.Poll)
	}
	res.Index, res.Kind = index, st.Op
	if res.Title == "" {
		res.Title = title
	}
	r.report.Asserts = append(r.report.Asserts, res)
	r.sink(res)
}

// waitForMirror blocks until the mirror node has ingested the newest
// transaction, so assertions never read state from before it.
func (r *run) waitForMirror() {
	if r.lastTx == "" || r.lastTx == r.syncedTx {
		return
	}
	deadline := time.Now().Add(r.opts.MirrorSync)
	announced := false
	for time.Now().Before(deadline) && r.ctx.Err() == nil {
		_, _, err := r.target.Mirror.Transaction(r.ctx, r.lastTx)
		if err == nil {
			r.syncedTx = r.lastTx
			return
		}
		if !errors.Is(err, mirror.ErrNotFound) {
			r.sink(event.Log{Level: "warn", Msg: "mirror: " + err.Error()})
		}
		if !announced && r.target.Mode != network.Mock {
			r.sink(event.Log{Level: "info", Msg: "waiting for the mirror node to catch up"})
			announced = true
		}
		time.Sleep(r.opts.Poll.Interval)
	}
	r.sink(event.Log{Level: "warn", Msg: fmt.Sprintf("mirror did not show %s within %s", r.lastTx, r.opts.MirrorSync)})
	r.syncedTx = r.lastTx
}

func newRunID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
