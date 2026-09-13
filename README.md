# hh, a hedera harness in go

[![ci](https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml)

hh runs YAML scenarios against Hedera and proves the result on the mirror node.
It ships with a mock Hedera network that runs in process, so a full token,
topic or schedule flow runs in milliseconds with no docker, no portal account
and no testnet hbar. The same scenarios run on testnet: all 7 examples pass on real testnet
([results and hashscan links](#verified-on-testnet)).

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
| six different env var names for the operator across snippets | all of them accepted, `.env` loaded automatically |

## quick start

needs go 1.26.

```sh
git clone https://github.com/Gmin2/hedera-harness-go && cd hedera-harness-go
go build -o hh . && export PATH="$PWD:$PATH"

hh run examples/            # every example, on the mock network, no keys needed
hh                          # the interactive ui
```

or `go install github.com/Gmin2/hedera-harness-go@latest` (the binary is then named `hedera-harness-go`).

On testnet, put a [portal](https://portal.hedera.com) account in `.env` (`HEDERA_OPERATOR_ID`, `HEDERA_OPERATOR_KEY`), then:

```sh
hh doctor -n testnet        # key type, key matches the account, balance, mirror and node reachable
hh run examples/ -n testnet
```

Start a project:

```sh
hh init my-project && cd my-project     # hh.yaml, scenarios/first-transfer.yaml, .env.example
hh run                                  # runs the judges listed in hh.yaml
hh                                      # tui with those judges preloaded
```

`hh.yaml` is the one place a project declares its network, scenarios, judges and agent settings. `hh run`, `hh check`, `hh agent` and the tui all read it, flags override it:

```yaml
network: mock
scenarios: [scenarios]
agent: { model: sonnet, max_cost: 2, max_attempts: 3, timeout: 20m }
judge:
  scenarios: [scenarios/first-transfer.yaml]
  checks:
    - go test ./...                                     # any shell command that must exit 0
    - { name: contracts, run: yarn hardhat:test, timeout: 5m }
```

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

## agent mode

Like [hedera-dev/hedera-harness](https://github.com/hedera-dev/hedera-harness), hh can put a coding agent to work and decide itself whether the work passed. It drives the `claude` cli (your existing claude code login, no api key), streams every message and tool call, then runs the judges: checks first (shell commands like a build or tests, `--check` or `judge.checks`), then scenarios. A failing judge sends its findings back into the same claude session as a repair prompt.

```sh
hh agent "write stablecoin.yaml: token USDX with 2 decimals and a kyc key. alice gets kyc and 250.00 USDX, a transfer to bob without kyc must fail" \
  --judge stablecoin.yaml --model haiku
```

```
 Attempt 1 · claude ───────────────────────────────────────────
  │ write stablecoin.yaml: token USDX with 2 decimals ...
   ✓ Write stablecoin.yaml
   ✓ Bash hh check stablecoin.yaml
   ✓ Bash hh run stablecoin.yaml --network mock --json
   ◇ claude-haiku-4-5 · 4 turns · $0.07 · 45.7s

  hh  stablecoin kyc test · mock
   ✓ token.create usdx (symbol=USDX, supply=50000, decimals=2, kyc=issuer)
   ✓ token.transfer usdx (from=issuer, to=bob, amount=25000)
     expected ACCOUNT_KYC_NOT_GRANTED_FOR_TOKEN
    PASS  alice usdx balance   = 25000   actual 25000

  PASS  judge passed 1/1 scenarios
 ✓ done in 1 attempt(s) · $0.07 · 45.8s
```

| | hedera-harness | hh agent |
|---|---|---|
| agent | claude or cursor cli, stream-json | claude cli, stream-json |
| who decides pass | harness validators, chain checks by an llm reading curl output | hh scenarios evaluated in code against the mirror node |
| agent feedback | after the whole attempt | the agent can run `hh check` and `hh run` on the mock in milliseconds while it works |
| repair | new prompt from findings | findings go back into the same session with `--resume` |

The agent gets a system prompt generated from the op and assertion registries, so the scenario reference it sees is always the one the code accepts.

It is a conversation, like claude code. In the tui (`./hh`) type a question or a task, the answer streams in, and every follow up continues the same claude session. `/judge <scenario>` picks what the work must pass, `/new` starts over, esc cancels. From the cli, `hh agent --resume <session> "..."` continues where the last run left off.

| flag / env | does |
|---|---|
| `--model`, `HH_AGENT_MODEL` | claude model, eg `haiku` for quick demos |
| `--max-cost`, `HH_AGENT_MAX_COST` | stop the loop once attempts cost this many usd (also passed to claude as `--max-budget-usd`) |
| `--timeout` | longest one attempt may run, default 20m |
| `--max-attempts` | attempts including repairs, default 3 |

## networks

| mode | what it is | needs |
|---|---|---|
| `mock` (default) | in process fake consensus node (grpc) and mirror node (rest + grpc topic stream) over one ledger. a normal sdk client talks to it, nothing is stubbed inside the sdk | nothing |
| `local` | [solo](https://solo.hiero.org) on `127.0.0.1:35211`, mirror rest `:38081`. `HH_LOCAL_PROFILE=localnode` for hiero local node (deprecated after sept 2026) | a running solo |
| `testnet` | hedera testnet, links to hashscan | `HEDERA_OPERATOR_ID`, `HEDERA_OPERATOR_KEY` |

### build a dapp by describing it

```sh
hh init --scaffold-hbar my-dapp --install   # scaffold-hbar + hh.yaml + the official hedera skills in .claude/skills
cd my-dapp && cp .env.example .env          # your portal account deploys
hh                                          # type: build a page where I can swap HBAR for USDC at a fixed rate
```

In a scaffold-hbar project the agent switches to dapp mode: claude gets a hedera dapp system prompt (project layout, tinybar vs weibar, hashio gas, HTS association, when to use HTS or ERC20) and the hedera-token-service, hedera-consensus-service, hts-system-contract and hss-system-contract skills. It writes the contracts, deploy scripts and the Next.js page. It cannot deploy and cannot read `.env`: after each attempt hh compiles, type checks and deploys to testnet with the connected wallet, and sends any failure back to the same session. When everything passes, run `yarn next:start` and open http://localhost:3000.

### wallets

Like connecting a wallet in a dapp, hh has one connected wallet that pays for and signs every scenario run, judge run and repair loop. Open the picker with ctrl+w (or `/wallet`) in the tui, `hh wallet` in the cli, or set it in hh.yaml.

| wallet | what |
|---|---|
| default | the network operator: the in process mock operator, solo genesis, or your portal account from .env |
| burner | a fresh ecdsa account funded from the default one for each run and swept back after, like the throwaway signer of the original harness (`wallet: { kind: burner, fund: 50 }`) |
| import | paste an account id and key in the tui, kept for the session only |

Connecting checks the wallet first: the key parses, the account exists, the key matches it, and the balance is enough. Checks (build, test, deploy commands) get the wallet as `HH_OPERATOR_ID`, `HH_OPERATOR_KEY`, `HH_JSON_RPC_URL` and, for ecdsa keys, `__RUNTIME_DEPLOYER_PRIVATE_KEY` so scaffold-hbar deploys sign with it. The agent never receives a key: hh strips every `*PRIVATE_KEY*` and `*OPERATOR_KEY*` variable from the environment claude runs in, and judge scenarios that exist before a run are pinned, so the agent cannot rewrite them to pass.

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
| `hh init [dir]` | create hh.yaml, a starter scenario and .env.example |
| `hh agent <prompt> --judge <file>` | let claude code do a task, judge it with scenarios, repair until it passes |
| `hh check <file\|dir>...` | validate scenarios offline: unknown fields, unknown names, steps that use a name before it exists |
| `hh wallet` | list the wallets a network offers, with balance and checks |
| `hh doctor` | preflight a network and operator before spending anything |
| `hh ops` | list step ops and assertions |
| `hh mock` | keep a mock network running for other tools (prints addresses and operator key) |

## steps and assertions

steps: `hbar.transfer`, `account.update`, `token.create` (fungible, nft, keys, fixed/fractional/royalty fees), `token.associate`, `token.mint`, `token.transfer`, `token.airdrop` (with `recipients`), `token.claim`, `token.cancel`, `token.reject`, `token.grant_kyc`, `token.revoke_kyc`, `token.freeze`, `token.unfreeze`, `token.pause`, `token.unpause`, `topic.create`, `topic.submit`, `schedule.create` (wraps a transfer, token transfer, mint, associate or topic submit), `schedule.sign`

assertions: `account.hbar`, `token.balance`, `token.supply`, `token.relationship` (associated, kyc, freeze, automatic), `token.paused`, `nft.owner`, `topic.messages` (count, contains, sequence, `verify_chain` recomputes the v3 running hash of every message), `schedule.executed` (also checks the inner transaction result, so a schedule that ran but reverted does not pass), `airdrop.pending`, `contract.call` (a read only call through the mirror node, contract by hardhat deployment name, 0x address or 0.0.x id, actor names as address args, eg `{ contract: HederaToken, function: "balanceOf(address)", args: [alice], equals: 10000e18 }`, local and testnet only)

numeric assertions take `equals`, `not`, `gt`, `gte`, `lt`, `lte`.

## before and after

Ports of [hedera-code-snippets](https://github.com/hedera-dev/hedera-code-snippets), counting lines of code without blanks and comments:

| flow | snippet | hh scenario | notes |
|---|---|---|---|
| airdrop, claim, reject, cancel | 214 (`airdrop.js` + utils) | 40 (`examples/03-airdrop.yaml`) | the snippet prints balances, hh asserts 9 facts |
| permissioned topic writes | 99 (`hcs-write.js`, plus 117 in `bip39-create-accounts` to make its accounts) | 20 (`examples/04-private-topic.yaml`, creates its own accounts and verifies the running hash chain) | |
| multisig account | 81 (`multisig-1-of-2.js`) | 20 (`examples/05-scheduled-multisig.yaml`, also schedules the payout) | |
| create and mint a token | 44 (`create-and-mint.js`) | one `token.create` step (see the scenario at the top) | |

Each hh scenario runs on the mock in tens of milliseconds with no account, and the same file runs on testnet in 15 to 35 seconds.

## verified on testnet

hh from main, portal account 0.0.10522535, 2026-09-13:

| example | result | time |
|---|---|---|
| 01-first-transfer | passed, 1 step, 2 assertions ([transfer](https://hashscan.io/testnet/transaction/0.0.10522535@1789306958.719056876)) | 14.8s |
| 02-token-kyc | passed, 11 steps incl kyc, freeze and pause gates, 5 assertions ([token create](https://hashscan.io/testnet/transaction/0.0.10522535@1789307010.887486229)) | 31.1s |
| 03-airdrop | passed, airdrop to 4 accounts, claim, reject, cancel, 9 assertions ([airdrop](https://hashscan.io/testnet/transaction/0.0.10522535@1789307070.026535393)) | 34.3s |
| 04-private-topic | passed, `verify_chain` recomputed the running hash on the real topic ([topic create](https://hashscan.io/testnet/transaction/0.0.10522535@1789306990.084980032)) | 24.6s |
| 05-scheduled-multisig | passed, 2 of 3 schedule executed with inner tx SUCCESS ([executing signature](https://hashscan.io/testnet/transaction/0.0.10522535@1789307107.366004425)) | 26.8s |
| 06-nft-collection | passed, NO_REMAINING_AUTOMATIC_ASSOCIATIONS gate | 17.6s |
| 07-custom-fees | passed, fixed hbar plus 10 percent fractional fee assessed on chain ([fee transfer](https://hashscan.io/testnet/transaction/0.0.10522535@1789307899.955539328)) | 25.9s |
| 01 with `--wallet burner` | passed, burner [0.0.10524023](https://hashscan.io/testnet/account/0.0.10524023) created, used, deleted and swept back | 8.1s |

The testnet run also found two things the mock hid, both fixed: fee collectors must sign token create, and nft mints must sign with the supply key.

## compared to hedera-harness

| | hedera-dev/hedera-harness | hh |
|---|---|---|
| language | typescript | go on hiero-sdk-go |
| who decides pass | validators, chain checks by an llm reading curl output | scenarios evaluated in code against the mirror node, with evidence |
| networks | testnet | mock in process, solo, testnet |
| agent | claude or cursor, batch run | claude, a streaming conversation with resume |
| judges | build, playwright smoke, llm evaluation, chain | shell checks, scenarios, `contract.call` |
| wallet | ephemeral testnet signer | connected wallet: portal, burner per run, import |
| app target | scaffold-hbar web app, with browser checks | scaffold-hbar via `hh init --scaffold-hbar`, no browser checks yet |

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

- browser smoke checks for scaffold-hbar pages, so a dapp feature is judged end to end like the original harness
- tell infrastructure failures (mirror down, funding) apart from agent mistakes so they do not use repair attempts
- more agent clis (codex, cursor) behind the same stream parser
- more services: file service, allowances, contract writes
- mock fidelity: fees, nested custom fees, fidelity tests that diff mock and testnet runs
