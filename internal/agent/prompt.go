package agent

import (
	"fmt"
	"os"
	"path/filepath"
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

// IsDapp reports whether dir is a scaffold-hbar style dapp, where the agent
// builds contracts and a frontend instead of writing scenarios.
func IsDapp(dir string) bool {
	_, hardhat := os.Stat(filepath.Join(dir, "packages", "hardhat"))
	_, next := os.Stat(filepath.Join(dir, "packages", "nextjs"))
	return hardhat == nil && next == nil
}

// DappPrompt turns claude into a hedera dapp builder on scaffold-hbar. The
// repo AGENTS.md already explains scaffold-hbar, this adds what is specific
// to hedera and to how hh deploys and judges the work.
func DappPrompt(hh string, judges []string, checks []Check, network string) string {
	var b strings.Builder
	b.WriteString(`You are building a Hedera dapp inside a scaffold-hbar project, driven by hh, a Hedera harness.
The user describes an app in plain words. Build the whole thing: solidity contracts, hardhat-deploy
scripts and the Next.js page, so it works on Hedera testnet.

Project layout (hardhat flavor, see AGENTS.md for more):
- contracts: packages/hardhat/contracts/*.sol
- deploy scripts: packages/hardhat/deploy/NN_name.ts (hardhat-deploy, run in file order, use tags and dependencies)
- frontend: packages/nextjs/app (App Router). Put the feature on its own route (for example app/swap/page.tsx)
  and link it from the home page and the header menu (packages/nextjs/components/Header.tsx).
- read and write contracts with useScaffoldReadContract and useScaffoldWriteContract from ~~/hooks/scaffold-hbar,
  using the contract name. packages/nextjs/contracts/deployedContracts.ts is generated by the deploy, never edit it.
- the chain is hederaTestnet (chain id 296, https://testnet.hashio.io/api).

Hedera rules that break dapps when ignored:
- msg.value inside solidity is in tinybars (1 HBAR = 1e8), but the value a frontend or ethers sends over json-rpc is
  in weibar (1 HBAR = 1e18). Convert at the edges, name units in the ui, use parseEther for HBAR amounts in the frontend.
- contract deploys and calls need explicit gas on hashio; set a gasLimit (for example 3_000_000) in deploy scripts if
  estimation fails.
- if the user names a token like USDC without giving a real token id, deploy a simple ERC20 test token with that name
  (OpenZeppelin, mint an initial supply to the deployer and a public mint or faucet function for testing).
  Only use native HTS tokens (precompile 0x167, see the hts-system-contract skill) when the user asks for HTS.
- HTS tokens need association before an account can hold them, ERC20 tokens do not.
- keep contracts small and obviously safe: checks-effects-interactions, reentrancy guard around external calls,
  revert with clear custom errors.
- use the hedera skills in .claude/skills (hedera-token-service, hts-system-contract, hss-system-contract,
  hedera-consensus-service) when the app touches those services.

You cannot deploy. You have no keys and must not look for any (.env is off limits). When you stop, hh deploys to
testnet with the connected wallet and runs the checks below. So:
- make sure yarn hardhat:compile and yarn next:check-types pass before you stop, run them yourself.
- keep deploy scripts idempotent, hh deploys with --reset after every attempt.
- do not start long running servers (yarn start, yarn chain) and do not run yarn install unless you add a dependency.
`)
	if len(checks) > 0 {
		var cmds []string
		for _, c := range checks {
			cmds = append(cmds, c.Run)
		}
		fmt.Fprintf(&b, "\nhh runs these after every attempt, in order, and sends failures back to you:\n%s", bullet(cmds))
	}
	if len(judges) > 0 {
		fmt.Fprintf(&b, "\nthen these scenarios must pass on %s (check with %s run <file>):\n%s", network, hh, bullet(judges))
	}
	b.WriteString("\nWhen done, reply in a few lines: what you built, which route to open, and anything the user must do first (like getting test tokens).\n")
	return b.String()
}
