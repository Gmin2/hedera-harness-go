# hh

<p align="center">
    <img width="800" alt="hh demo" src="demo/assets/hh.gif" /><br />
    <a href="https://github.com/Gmin2/hedera-harness-go/releases"><img src="https://img.shields.io/github/v/release/Gmin2/hedera-harness-go" alt="Latest Release"></a>
    <a href="https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml"><img src="https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-6B50FF" alt="License"></a>
</p>

<p align="center">Claude Code for Hedera.<br />Describe a dapp, hh builds it, deploys it to testnet, and a harness decides whether it works.</p>

## Features

- **Build dapps by describing them:** type "build a page where I can swap HBAR for USDC" and claude builds the contracts, deploy scripts and Next.js page on scaffold-hbar, with the official hedera skills loaded
- **A harness, not a vibe check:** after every attempt hh compiles, type checks and deploys to testnet, runs yaml scenarios against the mirror node, and sends failures back into the same claude session
- **Proven on chain, in code:** assertions read the mirror node and poll until they match, with the url and last value as evidence. No llm reading curl output
- **Mock network in process:** a fake consensus node and mirror node that the real hiero-sdk-go client talks to. Full token, topic and schedule flows in milliseconds, no docker, no keys
- **Same file on testnet:** all seven examples verified on real testnet, [with hashscan links](#verified-on-testnet)
- **Connected wallet:** a picker like a dapp (ctrl+w) for your portal account, a burner funded per run and swept back, or an imported key. The agent never holds a key
- **Conversation, not a batch job:** streaming answers, follow ups resume the session, `/new` to start over, cost and budget in the sidebar
- **Go, on the official sdk:** a single static binary for macOS, Linux and Windows

## Installation

Download a binary from [releases](https://github.com/Gmin2/hedera-harness-go/releases/latest):

```bash
# macOS apple silicon. use darwin_amd64, linux_amd64 or linux_arm64 for others
curl -sL https://github.com/Gmin2/hedera-harness-go/releases/latest/download/hh_darwin_arm64.tar.gz | tar xz
sudo mv hh /usr/local/bin/
```

Or with Go 1.26:

```bash
go install github.com/Gmin2/hedera-harness-go@latest   # the binary is named hedera-harness-go

git clone https://github.com/Gmin2/hedera-harness-go && cd hedera-harness-go
go build -o hh .
```

Agent mode uses the [claude cli](https://claude.com/claude-code) with your existing login, no api key.

## Getting Started

### Build a dapp

```bash
hh init --scaffold-hbar my-dapp --install   # scaffold-hbar, hh.yaml, hedera skills in .claude/skills
cd my-dapp && cp .env.example .env          # add a portal.hedera.com testnet account
hh
```

Then type what you want:

```
build a page where I can swap HBAR for USDC at a fixed rate of 1 HBAR = 0.05 USDC. deploy a USDC test token the swap contract can mint, add a faucet button, and show my HBAR and USDC balances
```

Claude writes the code. hh compiles it, type checks the frontend and deploys to testnet with the connected wallet, and every failure goes back to claude until it passes. Then run `yarn next:start` and open http://localhost:3000.

### Run scenarios

No account needed, everything runs on the in process mock:

```bash
hh run examples/            # every example scenario
hh                          # the interactive ui, ctrl+r to pick a scenario
```

<p align="center"><img width="800" alt="hh run" src="demo/assets/run.gif" /></p>

A scenario:

```yaml
name: kyc gated token
actors:
  issuer: { hbar: 20 }
  alice: { key: ed25519 }

steps:
  - token.create: { as: gold, name: Gold, symbol: GLD, decimals: 2, initial_supply: 100000, treasury: issuer, kyc_key: issuer }
  - token.associate: { account: alice, token: gold }
  - token.transfer: { token: gold, from: issuer, to: alice, amount: 500, expect: ACCOUNT_KYC_NOT_GRANTED_FOR_TOKEN }
  - token.grant_kyc: { token: gold, account: alice, key: issuer }
  - token.transfer: { token: gold, from: issuer, to: alice, amount: 500 }

assert:
  - token.balance: { account: alice, token: gold, equals: 500 }
  - token.relationship: { account: alice, token: gold, kyc: granted }
```

Actors become real accounts (ecdsa with an evm alias by default, ed25519, or `threshold: 2, keys: [a, b, c]`). hh picks the signers each transaction needs, `signers: [...]` overrides them for negative tests, and `expect:` turns a failure code into the expected result.

### On testnet

```bash
echo 'HEDERA_OPERATOR_ID=0.0.1234' >> .env
echo 'HEDERA_OPERATOR_KEY=0x...'   >> .env
hh doctor -n testnet        # key type, key matches the account, balance, mirror and node reachable
hh run examples/ -n testnet
```

## Configuration

`hh.yaml` is the one place a project declares its network, wallet, judges and agent. `hh run`, `hh check`, `hh agent` and the tui all read it, flags override it. `hh init` writes one.

```yaml
network: testnet
wallet: default                   # or burner, or { kind: burner, fund: 120 }
agent: { model: sonnet, max_cost: 5, max_attempts: 3, timeout: 30m }
judge:
  checks:                         # shell commands that must exit 0 after every attempt
    - { name: compile, run: yarn hardhat:compile, timeout: 5m }
    - { name: deploy to testnet, run: cd packages/hardhat && npx hardhat deploy --network hederaTestnet --reset }
  scenarios:                      # on chain checks against the mirror node
    - .hh/scenarios/swap.yaml
```

### Networks

| network | what | needs |
|---|---|---|
| `mock` | in process fake consensus node (grpc) and mirror node (rest and topic stream) over one ledger, used by a normal sdk client | nothing |
| `local` | [solo](https://solo.hiero.org) on `127.0.0.1:35211`, mirror rest `:38081`, or `HH_LOCAL_PROFILE=localnode` | a running solo |
| `testnet` | hedera testnet, hashscan links | `HEDERA_OPERATOR_ID`, `HEDERA_OPERATOR_KEY` |

Override addresses with `HH_CONSENSUS`, `HH_MIRROR_GRPC`, `HH_MIRROR_REST`. The mock charges no fees, so write `gte` or `lte` for accounts that pay fees when a scenario also runs on testnet.

### Wallets

| wallet | what |
|---|---|
| default | the network operator: mock operator, solo genesis, or your portal account from `.env` |
| burner | a fresh ecdsa account funded from the default one for each run, deleted and swept back after |
| import | an account id and key pasted in the tui, kept for the session only |

Connecting checks the wallet: the key parses, the account exists, the key matches it, the balance is enough. Checks get the wallet as `HH_OPERATOR_ID`, `HH_OPERATOR_KEY`, `HH_JSON_RPC_URL` and `__RUNTIME_DEPLOYER_PRIVATE_KEY`, so deploys sign with it. Claude never does: hh strips every key variable from its environment, denies it `.env`, and pins judge scenarios so it cannot rewrite them to pass.

### Agent

| flag or env | does |
|---|---|
| `--model`, `HH_AGENT_MODEL` | claude model, `haiku` is quickest |
| `--max-cost`, `HH_AGENT_MAX_COST` | stop once attempts cost this many usd, also passed to claude as `--max-budget-usd` |
| `--timeout` | longest one attempt may run |
| `--max-attempts` | attempts including repairs |
| `--resume` | continue an earlier conversation |

In the tui: type a prompt, `/judge <scenario>`, `/check <command>`, `/wallet`, `/network`, `/new`, esc to cancel.

## Commands

| command | does |
|---|---|
| `hh` | the tui |
| `hh init [dir]`, `hh init --scaffold-hbar [dir]` | start a project or a dapp |
| `hh agent <prompt>` | claude does the task, hh judges and repairs, `--judge`, `--check` |
| `hh run [file or dir]` | run scenarios, exit 1 on failure, `--json` for ci |
| `hh check [file or dir]` | validate scenarios offline, with line numbers |
| `hh wallet` | wallets a network offers, with balance and checks |
| `hh doctor` | preflight a network and operator before spending anything |
| `hh ops` | every step and assertion with its fields |
| `hh mock` | keep a mock network running for other tools |

## Steps and assertions

**Steps:** `hbar.transfer`, `account.update`, `token.create` (fungible, nft, keys, fixed, fractional and royalty fees), `token.associate`, `token.mint`, `token.transfer`, `token.airdrop`, `token.claim`, `token.cancel`, `token.reject`, `token.grant_kyc`, `token.revoke_kyc`, `token.freeze`, `token.unfreeze`, `token.pause`, `token.unpause`, `topic.create`, `topic.submit`, `schedule.create`, `schedule.sign`

**Assertions:** `account.hbar`, `token.balance`, `token.supply`, `token.relationship`, `token.paused`, `nft.owner`, `topic.messages` (with `verify_chain`, which recomputes the v3 running hash of every message), `schedule.executed` (also requires the inner transaction to succeed), `airdrop.pending`, `contract.call` (a read only call through the mirror node, by hardhat deployment name, address or id)

Numbers take `equals`, `not`, `gt`, `gte`, `lt`, `lte`. `hh ops` lists every field.

## Verified on testnet

hh from main, portal account 0.0.10522535, 2026-09-13:

| example | result | time |
|---|---|---|
| 01-first-transfer | passed ([transfer](https://hashscan.io/testnet/transaction/0.0.10522535@1789306958.719056876)) | 14.8s |
| 02-token-kyc | passed, kyc, freeze and pause gates ([token create](https://hashscan.io/testnet/transaction/0.0.10522535@1789307010.887486229)) | 31.1s |
| 03-airdrop | passed, airdrop to 4 accounts, claim, reject, cancel ([airdrop](https://hashscan.io/testnet/transaction/0.0.10522535@1789307070.026535393)) | 34.3s |
| 04-private-topic | passed, running hash chain verified on the real topic ([topic create](https://hashscan.io/testnet/transaction/0.0.10522535@1789306990.084980032)) | 24.6s |
| 05-scheduled-multisig | passed, 2 of 3 schedule executed with inner tx SUCCESS ([executing signature](https://hashscan.io/testnet/transaction/0.0.10522535@1789307107.366004425)) | 26.8s |
| 06-nft-collection | passed, NO_REMAINING_AUTOMATIC_ASSOCIATIONS gate | 17.6s |
| 07-custom-fees | passed, fixed and fractional fees assessed ([fee transfer](https://hashscan.io/testnet/transaction/0.0.10522535@1789307899.955539328)) | 25.9s |
| 01 with `--wallet burner` | passed, burner [0.0.10524023](https://hashscan.io/testnet/account/0.0.10524023) created, used, deleted | 8.1s |

The testnet run caught two things the mock hid, both fixed: fee collectors must sign token create, and nft mints must sign with the supply key.

## Compared to hedera-harness

hh takes direct inspiration from [hedera-dev/hedera-harness](https://github.com/hedera-dev/hedera-harness): the harness decides pass or fail, never the agent, and stages cost more as they go.

| | hedera-harness | hh |
|---|---|---|
| language | typescript | go on hiero-sdk-go |
| starting a feature | a recipe: spec.yaml, prd.md, validators, eval checklist | one sentence |
| agent | claude or cursor, unattended batch run | claude, a streaming conversation with resume |
| who decides pass | validators, chain checks by an llm reading curl output | shell checks and scenarios evaluated in code against the mirror node |
| networks | testnet | mock in process, solo, testnet |
| wallet | ephemeral testnet signer | connected wallet: portal, burner per run, import |
| ui checks | playwright smoke and an llm in a browser | not yet |

Lines of code for the same flows as [hedera-code-snippets](https://github.com/hedera-dev/hedera-code-snippets), without blanks and comments:

| flow | snippet | hh scenario |
|---|---|---|
| airdrop, claim, reject, cancel | 214 (`airdrop.js` and utils), prints balances | 40, asserts 9 facts |
| permissioned topic writes | 99 (`hcs-write.js`) plus 117 to create its accounts | 20, creates accounts and verifies the running hash |
| multisig account | 81 (`multisig-1-of-2.js`) | 20, also schedules the payout |

## How it works

```
prompt ─▶ claude (hedera skills, no keys) ─▶ code
                                              │
             checks: compile · types · deploy with the connected wallet
                                              │
             scenarios ─▶ hiero-sdk-go ─▶ mock | solo | testnet ─▶ mirror node ─▶ assertions
                                              │
                        failures back into the same session ─▶ repair
```

- `internal/agent`: drives the claude cli (stream-json), the judge and repair loop, dapp mode prompt
- `internal/mock`: ledger, fake consensus node that verifies ed25519, ecdsa, key list and threshold signatures, fake mirror in the openapi shapes
- `internal/ops`, `internal/assert`: yaml steps to sdk transactions, mirror assertions
- `internal/wallet`: default, burner and import wallets
- `internal/tui`: bubbletea v2, lipgloss v2, bubbles v2 and ultraviolet, laid out like [crush](https://github.com/charmbracelet/crush)

## Development

```bash
go test ./...                       # includes every example end to end on the mock network
demo/render.sh                      # re-record the readme gifs and demo clips with vhs
git tag v0.1.0 && git push --tags   # goreleaser builds and publishes the binaries
```

## Roadmap

- browser smoke checks for dapp pages, so ui features are judged end to end like the original harness
- telling infrastructure failures apart from agent mistakes, so they do not use repair attempts
- codex and cursor behind the same stream parser
- file service, allowances, contract writes, mock fees

## License

[Apache-2.0](LICENSE)
