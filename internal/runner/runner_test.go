package runner_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/mock"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/runner"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

func mockTarget(t *testing.T) *network.Target {
	t.Helper()
	srv, err := mock.Start(mock.Options{})
	if err != nil {
		t.Fatal(err)
	}
	id, key := srv.Operator()
	target, err := network.Connect(context.Background(), network.Config{
		Mode:        network.Mock,
		Consensus:   srv.ConsensusAddr(),
		MirrorGRPC:  srv.MirrorGRPCAddr(),
		MirrorREST:  srv.MirrorRESTURL(),
		OperatorID:  id.String(),
		OperatorKey: key.StringDer(),
	})
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		target.Close()
		srv.Close()
	})
	return target
}

func TestExamplesPassOnMock(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			sc, err := scenario.Load(f)
			if err != nil {
				t.Fatal(err)
			}
			if err := runner.Plan(sc); err != nil {
				t.Fatalf("plan: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rep := runner.Run(ctx, mockTarget(t), sc, runner.Defaults(network.Mock), nil)
			if rep.Status != event.Passed {
				t.Fatal(describe(rep))
			}
		})
	}
}

func TestFailuresAreReported(t *testing.T) {
	sc, err := scenario.Parse([]byte(`
name: broken on purpose
actors:
  alice: {}
  bob: {}
steps:
  - hbar.transfer: { from: alice, to: bob, amount: 1 }
  - hbar.transfer: { from: alice, to: bob, amount: 1, signers: [], expect: SUCCESS }
  - hbar.transfer: { from: alice, to: bob, amount: 1 }
assert:
  - account.hbar: { account: bob, equals: 999 }
`))
	if err != nil {
		t.Fatal(err)
	}
	var events []event.Event
	rep := runner.Run(context.Background(), mockTarget(t), sc, runner.Defaults(network.Mock), func(e event.Event) {
		events = append(events, e)
	})
	if rep.Status != event.Failed {
		t.Fatalf("want failed run, got %s", rep.Status)
	}
	if got := rep.Steps[1]; got.Status != event.Failed || got.Receipt != "INVALID_SIGNATURE" {
		t.Fatalf("step 2: %+v", got)
	}
	if rep.Steps[2].Status != event.Skipped {
		t.Fatalf("step 3 should be skipped after a failure, got %s", rep.Steps[2].Status)
	}
	if rep.Asserts[0].Status != event.Skipped {
		t.Fatalf("assertion should be skipped, got %s", rep.Asserts[0].Status)
	}
	if _, ok := events[len(events)-1].(event.RunFinished); !ok {
		t.Fatalf("last event should be RunFinished, got %T", events[len(events)-1])
	}
}

func TestAssertionFailureCarriesEvidence(t *testing.T) {
	sc, err := scenario.Parse([]byte(`
actors:
  bob: { hbar: 3 }
assert:
  - account.hbar: { account: bob, equals: 4 }
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := runner.Run(context.Background(), mockTarget(t), sc, runner.Defaults(network.Mock), nil)
	a := rep.Asserts[0]
	if a.Status != event.Failed || a.Actual != "3 ℏ" || !strings.Contains(a.Source, "/accounts/") || a.Attempts < 2 {
		t.Fatalf("assertion evidence: %+v", a)
	}
}

func TestPlanCatchesMistakes(t *testing.T) {
	sc, err := scenario.Parse([]byte(`
actors:
  alice: {}
steps:
  - token.associate: { account: alice, token: gold }
  - token.create: { as: gold, name: Gold, symbol: GLD }
  - hbar.transfer: { from: alice, to: nobody, amount: 1 }
assert:
  - token.balance: { account: alice, token: silver, equals: 1 }
`))
	if err != nil {
		t.Fatal(err)
	}
	err = runner.Plan(sc)
	if err == nil {
		t.Fatal("expected plan errors")
	}
	for _, want := range []string{`unknown token "gold"`, `unknown account "nobody"`, `unknown token "silver"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("plan error should mention %s, got:\n%v", want, err)
		}
	}
}

func describe(rep *runner.Report) string {
	var b strings.Builder
	b.WriteString("run " + string(rep.Status) + " " + rep.Error + "\n")
	for _, s := range rep.Steps {
		b.WriteString("  step " + s.Op + " " + string(s.Status) + " " + s.Receipt + " " + s.Error + "\n")
	}
	for _, a := range rep.Asserts {
		b.WriteString("  assert " + a.Title + " " + string(a.Status) + " want " + a.Expected + " got " + a.Actual + " " + a.Error + "\n")
	}
	return b.String()
}

// A schedule counts as executed even when its inner transaction fails, the
// false pass community prs chase on the original harness.
func TestScheduleExecutedChecksInnerResult(t *testing.T) {
	sc, err := scenario.Parse([]byte(`
actors:
  broke: { hbar: 1 }
  payee: {}
steps:
  - schedule.create:
      as: payout
      tx:
        hbar.transfer: { from: broke, to: payee, amount: 5 }
  - schedule.sign: { schedule: payout, signer: broke }
assert:
  - schedule.executed: { schedule: payout }
  - schedule.executed: { schedule: payout, result: INSUFFICIENT_ACCOUNT_BALANCE }
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := runner.Run(context.Background(), mockTarget(t), sc, runner.Defaults(network.Mock), nil)
	if len(rep.Asserts) != 2 {
		t.Fatal(describe(rep))
	}
	if a := rep.Asserts[0]; a.Status != event.Failed || !strings.Contains(a.Actual, "INSUFFICIENT_ACCOUNT_BALANCE") {
		t.Fatalf("a reverted schedule must not pass by default:\n%s", describe(rep))
	}
	if a := rep.Asserts[1]; a.Status != event.Passed {
		t.Fatalf("expecting the failure explicitly should pass:\n%s", describe(rep))
	}
}
