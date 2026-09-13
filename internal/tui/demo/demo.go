// Package demo fakes a scenario run so the tui can be built and looked at
// without the real runner or a network.
package demo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

// Beat is one scripted event and how long to wait before sending it.
type Beat struct {
	Delay time.Duration
	Event event.Event
}

// Launch plays the scripted run into sink, sleeping between events. It
// stops early when ctx is canceled.
func Launch(ctx context.Context, path, network string, sink event.Sink) error {
	start := time.Now()
	for _, b := range Script(path, network) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.Delay):
		}
		ev := b.Event
		switch e := ev.(type) {
		case event.RunStarted:
			e.At = time.Now()
			ev = e
		case event.RunFinished:
			e.Elapsed = time.Since(start).Round(100 * time.Millisecond)
			ev = e
		}
		sink(ev)
	}
	return nil
}

// Events is the script without the pauses, for tests.
func Events(path, network string) []event.Event {
	beats := Script(path, network)
	out := make([]event.Event, len(beats))
	for i, b := range beats {
		out[i] = b.Event
	}
	return out
}

// Script builds the fake run: three actors, eight steps with one expected
// failure and one real failure, six assertions with one failing.
func Script(path, network string) []Beat {
	return script(path, network, "7f3a2c", true)
}

// script builds the run above. With broken unset the scheduled payout is
// signed and everything passes, which is how the agent demo repairs it.
func script(path, network, runID string, broken bool) []Beat {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	operator := "0.0.2"
	if network == "testnet" {
		operator = "0.0.4512"
	}
	// Mock and local transactions do not exist on hashscan, but the demo
	// links them anyway so the ui has something to show.
	explorer := "testnet"
	if network == "mainnet" {
		explorer = network
	}

	seq := 0
	tx := func() string {
		seq++
		return fmt.Sprintf("%s@1757712000.%09d", operator, 100+seq)
	}
	hashscan := func(id string) string {
		return fmt.Sprintf("https://hashscan.io/%s/transaction/%s", explorer, id)
	}
	accountLink := func(id string) string {
		return fmt.Sprintf("https://hashscan.io/%s/account/%s", explorer, id)
	}
	mirror := "https://testnet.mirrornode.hedera.com"
	if network != "testnet" {
		mirror = "http://localhost:5551"
	}

	var beats []Beat
	add := func(d time.Duration, ev event.Event) {
		beats = append(beats, Beat{Delay: d, Event: ev})
	}
	param := func(kv ...string) []event.Param {
		var ps []event.Param
		for i := 0; i+1 < len(kv); i += 2 {
			ps = append(ps, event.Param{Key: kv[i], Value: kv[i+1]})
		}
		return ps
	}

	add(200*time.Millisecond, event.RunStarted{
		RunID:      runID,
		Scenario:   name,
		Path:       path,
		Network:    network,
		Operator:   operator,
		Actors:     3,
		Steps:      8,
		Assertions: 6,
		At:         time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	})

	for i, a := range []struct{ name, account, key, hbar string }{
		{"alice", "0.0.1001", "ed25519", "100"},
		{"bob", "0.0.1002", "ecdsa", "50"},
		{"carol", "0.0.1003", "ed25519", "10"},
	} {
		id := tx()
		add(time.Duration(300+i*150)*time.Millisecond, event.ActorReady{
			Name: a.name, Account: a.account, KeyType: a.key, Hbar: a.hbar,
			TxID: id, Link: accountLink(a.account),
		})
	}

	type step struct {
		op, target string
		params     []event.Param
		receipt    string
		expected   string
		status     event.Status
		entities   []event.Param
		err        string
		took       time.Duration
	}
	steps := []step{
		{op: "token.create", target: "gold", params: param("supply", "1000", "decimals", "2", "treasury", "alice"),
			receipt: "SUCCESS", status: event.Passed, entities: param("gold", "0.0.1004"), took: 900 * time.Millisecond},
		{op: "token.associate", target: "bob", params: param("token", "gold"),
			receipt: "SUCCESS", status: event.Passed, took: 450 * time.Millisecond},
		{op: "token.transfer", target: "gold", params: param("from", "alice", "to", "bob", "amount", "250"),
			receipt: "SUCCESS", status: event.Passed, took: 520 * time.Millisecond},
		{op: "token.transfer", target: "gold", params: param("from", "alice", "to", "carol", "amount", "10"),
			receipt: "TOKEN_NOT_ASSOCIATED_TO_ACCOUNT", expected: "TOKEN_NOT_ASSOCIATED_TO_ACCOUNT",
			status: event.Passed, took: 480 * time.Millisecond},
		{op: "hbar.transfer", params: param("from", "alice", "to", "bob", "amount", "5"),
			receipt: "SUCCESS", status: event.Passed, took: 400 * time.Millisecond},
		{op: "topic.create", target: "news", params: param("memo", "hh demo"),
			receipt: "SUCCESS", status: event.Passed, entities: param("news", "0.0.1005"), took: 610 * time.Millisecond},
		{op: "topic.submit", target: "news", params: param("message", "hello hedera"),
			receipt: "SUCCESS", status: event.Passed, took: 380 * time.Millisecond},
		{op: "schedule.create", target: "payout", params: param("payer", "bob", "to", "carol", "amount", "1"),
			receipt: "INVALID_SIGNATURE", expected: "SUCCESS", status: event.Failed,
			err: "bob did not sign the schedule create transaction", took: 700 * time.Millisecond},
	}
	if !broken {
		last := &steps[len(steps)-1]
		last.receipt, last.expected, last.status, last.err = "SUCCESS", "", event.Passed, ""
	}
	for i, s := range steps {
		add(250*time.Millisecond, event.StepStarted{Index: i, Op: s.op, Target: s.target, Params: s.params})
		if i == 5 {
			add(300*time.Millisecond, event.Log{Level: "info", Msg: "waiting for the mirror node to catch up"})
		}
		id := tx()
		add(s.took, event.StepFinished{
			Index: i, Op: s.op, Status: s.status, Receipt: s.receipt, Expected: s.expected,
			TxID: id, Link: hashscan(id), Entities: s.entities, Error: s.err, Elapsed: s.took,
		})
	}

	add(200*time.Millisecond, event.Log{Level: "warn", Msg: "mirror node is 2 blocks behind, assertions may retry"})

	type check struct {
		kind, title, expected, actual, source string
		status                                event.Status
		attempts                              int
		err                                   string
		took                                  time.Duration
	}
	checks := []check{
		{kind: "token.supply", title: "gold", expected: "1000", actual: "1000",
			source: "/api/v1/tokens/0.0.1004", status: event.Passed, attempts: 1, took: 320 * time.Millisecond},
		{kind: "token.balance", title: "bob gold", expected: "250", actual: "250",
			source: "/api/v1/accounts/0.0.1002/tokens", status: event.Passed, attempts: 2, took: 810 * time.Millisecond},
		{kind: "token.balance", title: "carol gold", expected: "0", actual: "0",
			source: "/api/v1/accounts/0.0.1003/tokens", status: event.Passed, attempts: 1, took: 290 * time.Millisecond},
		{kind: "hbar.balance", title: "bob", expected: ">= 55", actual: "55",
			source: "/api/v1/balances?account.id=0.0.1002", status: event.Passed, attempts: 1, took: 260 * time.Millisecond},
		{kind: "topic.messages", title: "news", expected: "1", actual: "1",
			source: "/api/v1/topics/0.0.1005/messages", status: event.Passed, attempts: 1, took: 300 * time.Millisecond},
		{kind: "schedule.executed", title: "payout", expected: "executed", actual: "not found",
			source: "/api/v1/schedules?account.id=0.0.1002", status: event.Failed, attempts: 3,
			err: "no schedule created by 0.0.1002 after 3 attempts", took: 1500 * time.Millisecond},
	}
	if !broken {
		last := &checks[len(checks)-1]
		last.actual, last.status, last.attempts, last.err = "executed", event.Passed, 1, ""
	}
	for i, c := range checks {
		add(150*time.Millisecond, event.AssertionStarted{Index: i, Kind: c.kind, Title: c.title})
		add(c.took, event.AssertionFinished{
			Index: i, Kind: c.kind, Title: c.title, Status: c.status,
			Expected: c.expected, Actual: c.actual, Source: mirror + c.source,
			Attempts: c.attempts, Error: c.err, Elapsed: c.took,
		})
	}

	finished := event.RunFinished{
		RunID:      runID,
		Status:     event.Failed,
		StepsOK:    7,
		StepsFail:  1,
		AssertOK:   5,
		AssertFail: 1,
		Elapsed:    11600 * time.Millisecond,
	}
	if !broken {
		finished.Status = event.Passed
		finished.StepsOK, finished.StepsFail = 8, 0
		finished.AssertOK, finished.AssertFail = 6, 0
		finished.Elapsed = 10900 * time.Millisecond
	}
	add(200*time.Millisecond, finished)
	return beats
}
