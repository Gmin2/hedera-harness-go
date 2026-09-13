# hh, a hedera harness in go

[![ci](https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml)

hh runs YAML scenarios against Hedera and proves the result on the mirror node.
It ships with a mock Hedera network that runs in process, so a full token,
topic or schedule flow runs in milliseconds with no docker, no portal account
and no testnet hbar. The same scenario then runs unchanged on solo or testnet.

It takes direct inspiration from [hedera-dev/hedera-harness](https://github.com/hedera-dev/hedera-harness)
(stages that cost more as they go, the harness decides pass or fail, never the agent)
and targets the gaps a first hour with it shows:

| pain today | hh |
|---|---|
| on chain checks are a sentence in an llm prompt ("verify via mirror node") | typed assertions evaluated in code, with the mirror url and last observed value as evidence |
| testnet is hardcoded, every chain check needs portal keys and faucet hbar | `mock`, `local` and `testnet` modes behind one code path |
| mirror lag handled with `sleep(6)` | assertions poll, and wait until the mirror has the last transaction before reading |
| typescript only | go, on the official hiero-sdk-go |
| `PrivateKey.fromString` silently turns an ecdsa hex key into ed25519 | keys never guess, ambiguous raw keys are resolved by asking the mirror node for the account key type |
| 7 different env var names for the operator across snippets | all of them accepted, `.env` loaded automatically |

## quick start

```sh
git clone https://github.com/Gmin2/hedera-harness-go && cd hedera-harness-go
go build -o hh .

./hh run examples/          # every example, on the mock network
./hh                        # the interactive ui
```

or without cloning: `go install github.com/Gmin2/hedera-harness-go@latest` (the binary is named `hedera-harness-go`).

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

- actors become real accounts (ecdsa with evm alias by default, ed25519, or `threshold: 2, keys: [a, b, c]`)
- hh picks the signers each transaction needs, `signers: [...]` overrides them for negative tests
- `expect:` turns a failure code into the expected outcome
- assertions can sit between steps or in `assert:` at the end

## networks

| mode | what it is | needs |
|---|---|---|
| `mock` (default) | in process fake consensus node (grpc) and mirror node (rest + grpc topic stream) over one ledger. a normal sdk client talks to it, nothing is stubbed inside the sdk | nothing |
| `local` | [solo](https://solo.hiero.org) on `127.0.0.1:35211`, mirror rest `:38081`. `HH_LOCAL_PROFILE=localnode` for hiero local node (deprecated after sept 2026) | a running solo |
| `testnet` | hedera testnet, links to hashscan | `HEDERA_OPERATOR_ID`, `HEDERA_OPERATOR_KEY` |

```sh
hh doctor --network testnet   # key type, key matches account, balance, mirror and node reachable
hh run examples/02-token-kyc.yaml --network testnet
```

Override addresses with `HH_CONSENSUS`, `HH_MIRROR_GRPC`, `HH_MIRROR_REST`.

The mock charges no fees so hbar assertions are exact. Write `gte`/`lte` for accounts that pay fees if the scenario also runs on testnet.

## commands

| command | does |
|---|---|
| `hh` | interactive ui: pick a scenario (ctrl+r), switch network (ctrl+n), watch it run |
| `hh run <file\|dir>...` | run scenarios, exit 1 on any failure. `--json` for ci, `--keep-going`, `--timeout` |
| `hh check <file\|dir>...` | validate scenarios offline: unknown fields, unknown names, steps that use a name before it exists |
| `hh doctor` | preflight a network and operator before spending anything |
| `hh ops` | list step ops and assertions |
| `hh mock` | keep a mock network running for other tools (prints addresses and operator key) |

## steps and assertions

steps: `hbar.transfer`, `account.update`, `token.create` (fungible, nft, keys, fixed/fractional/royalty fees), `token.associate`, `token.mint`, `token.transfer`, `token.airdrop` (with `recipients`), `token.claim`, `token.cancel`, `token.reject`, `token.grant_kyc`, `token.revoke_kyc`, `token.freeze`, `token.unfreeze`, `token.pause`, `token.unpause`, `topic.create`, `topic.submit`, `schedule.create` (wraps a transfer, token transfer, mint, associate or topic submit), `schedule.sign`

assertions: `account.hbar`, `token.balance`, `token.supply`, `token.relationship` (associated, kyc, freeze, automatic), `token.paused`, `nft.owner`, `topic.messages` (count, contains, sequence), `schedule.executed`, `airdrop.pending`

numeric assertions take `equals`, `not`, `gt`, `gte`, `lt`, `lte`.

## before and after

Ports of [hedera-code-snippets](https://github.com/hedera-dev/hedera-code-snippets), counting lines of code without blanks and comments:

| flow | snippet | hh scenario | notes |
|---|---|---|---|
| airdrop, claim, reject, cancel | 214 (`airdrop.js` + utils) | 40 (`examples/03-airdrop.yaml`) | the snippet prints balances, hh asserts 9 facts |
| permissioned topic writes | 216 (`hcs-write.js` + `bip39-create-accounts`) | 19 (`examples/04-private-topic.yaml`) | |
| multisig account | 81 (`multisig-1-of-2.js`) | 20 (`examples/05-scheduled-multisig.yaml`, also schedules the payout) | |
| create and mint a token | 44 (`create-and-mint.js`) | 8 lines of one step | |

Each hh scenario also runs offline in 10 to 20 ms, where the snippet needs a funded testnet account and a network round trip per transaction.

## how it works

```
scenario.yaml ─▶ runner ─▶ ops (yaml → sdk tx, signers) ─▶ hiero-sdk-go client ─▶ mock | solo | testnet
                   │                                                                    │
                   └─▶ assert (poll mirror rest, evidence) ◀──────── mirror node ◀──────┘
                   │
                   └─▶ events ─▶ plain printer | json | tui
```

- `internal/mock`: ledger, fake consensus node (generic grpc handlers that decode `SignedTransaction`, verify ed25519 and ecdsa signatures, key lists and thresholds, then apply to the ledger), fake mirror rest in the openapi shapes
- `internal/ops`: one file per service, each step builds a sdk transaction and names its signers
- `internal/assert`: checks read mirror data only, since the consensus balance query is gone (throttled to zero on testnet in aug 2026)
- `internal/tui`: bubbletea v2, lipgloss v2, bubbles v2 and ultraviolet, laid out like charmbracelet crush

## development

```sh
go run .               # same as hh
go test ./...          # includes every example scenario end to end on the mock network
go run ./cmd/hh-tui-demo
```

## roadmap

- agent loop: generate and repair a scaffold-hbar feature with a coding agent, judged by hh scenarios instead of an llm
- more services: file service, contracts through the json-rpc relay, allowances
- mock fidelity tests that run the same scenario on mock and solo and diff the results
