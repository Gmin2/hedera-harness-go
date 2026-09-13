package agent

import (
	"fmt"
	"strings"

	"github.com/Gmin2/hedera-harness-go/internal/assert"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
)

// SystemPrompt teaches the agent the scenario format and how to check its
// own work with hh before it stops.
func SystemPrompt(hh string, judges []string, checks []Check, network string) string {
	var b strings.Builder
	b.WriteString(`You are working inside hh, a Hedera harness. hh runs YAML scenarios against Hedera
(an in process mock network, solo, or testnet) and checks results on the mirror node.
hh, not you, decides whether the work passed.

Scenario format:

name: kyc gated token
actors:                      # accounts hh creates. key: ecdsa (default) or ed25519, hbar: initial balance (default 10)
  issuer: { hbar: 20 }
  alice: { key: ed25519, max_auto_associations: -1 }
  vault: { threshold: 2, keys: [issuer, alice], hbar: 5 }   # threshold key account
steps:                       # run in order, each is one op
  - token.create: { as: gold, name: Gold, symbol: GLD, decimals: 2, initial_supply: 1000, treasury: issuer, kyc_key: issuer }
  - token.associate: { account: alice, token: gold }
  - token.transfer: { token: gold, from: issuer, to: alice, amount: 5, expect: ACCOUNT_KYC_NOT_GRANTED_FOR_TOKEN }
  - token.balance: { account: alice, token: gold, equals: 0 }   # assertions may sit between steps
assert:                      # checked on the mirror node after the steps
  - token.supply: { token: gold, equals: 1000 }

Rules:
- names from actors and "as:" are referenced by later steps. "operator" is the paying account.
- hh signs with the keys each step needs. "signers: [a, b]" overrides that, for negative tests.
- "expect: STATUS" makes a failure code the expected outcome. Default is SUCCESS.
- hbar amounts are in hbar, token amounts in the smallest unit.
- schedule.create wraps one op: schedule.create: { as: pay, tx: { hbar.transfer: { from: vault, to: alice, amount: 1 } } }
- custom_fees on token.create: [{ fixed: { hbar: 1, collector: x } }, { fractional: { numerator: 1, denominator: 10, collector: x } }, { royalty: { numerator: 1, denominator: 20, fallback_hbar: 1, collector: x } }]
- numeric assertions take equals, not, gt, gte, lt, lte. the mock charges no fees, so hbar balances are exact there.

Steps and their fields:
`)
	for _, n := range ops.Names() {
		fmt.Fprintf(&b, "  %s: %s\n", n, strings.Join(ops.Fields(n), ", "))
	}
	b.WriteString("\nAssertions and their fields:\n")
	for _, n := range assert.Names() {
		fmt.Fprintf(&b, "  %s: %s\n", n, strings.Join(assert.Fields(n), ", "))
	}

	fmt.Fprintf(&b, `
Checking your work:
- validate without a network: %[1]s check <file.yaml>
- run on the mock network in milliseconds: %[1]s run <file.yaml> --network mock --json
- list ops: %[1]s ops
`, hh)
	if len(judges) > 0 {
		fmt.Fprintf(&b, `
When you stop, hh judges your work by running these scenarios on the %s network:
%s
They must pass. Run them yourself with %s run <file> before you finish. Do not weaken
an assertion to make it pass unless the task says the assertion is wrong.
`, network, bullet(judges), hh)
	}
	if len(checks) > 0 {
		var cmds []string
		for _, c := range checks {
			cmds = append(cmds, c.Run)
		}
		fmt.Fprintf(&b, `
These commands must also exit 0 in the project directory when you stop:
%s
Run them before you finish.
`, bullet(cmds))
	}
	b.WriteString("\nKeep the final reply short: what you changed and the check you ran.\n")
	return b.String()
}

// RepairPrompt asks the agent to fix what the judge found, in the same session.
func RepairPrompt(findings []string, judges []string, checks []Check, hh, network string) string {
	var b strings.Builder
	b.WriteString("hh judged your work and it did not pass. Findings:\n")
	b.WriteString(bullet(findings))
	fmt.Fprintf(&b, "\nFix the cause, then confirm with:\n")
	for _, c := range checks {
		fmt.Fprintf(&b, "  %s\n", c.Run)
	}
	for _, j := range judges {
		fmt.Fprintf(&b, "  %s run %s --network %s\n", hh, j, network)
	}
	b.WriteString("Stop when they pass.\n")
	return b.String()
}

func bullet(items []string) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- " + it + "\n")
	}
	return b.String()
}
