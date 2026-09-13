# hh guide

Everything the [README](../README.md) leaves out.

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

## Development

```sh
go test ./...                       # includes every example end to end on the mock network
demo/render.sh                      # re-record the readme gifs and demo clips with vhs
git tag v0.1.0 && git push --tags   # goreleaser publishes the binaries
```
