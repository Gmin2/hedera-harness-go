# hh

<p>
  <img src="demo/assets/hh-iso.png" width="500" alt="hh" />
  <br>
  <a href="https://github.com/Gmin2/hedera-harness-go/releases"><img src="https://img.shields.io/github/v/release/Gmin2/hedera-harness-go" alt="Latest Release"></a>
  <a href="https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml"><img src="https://github.com/Gmin2/hedera-harness-go/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-6B50FF" alt="License"></a>
</p>

Claude Code for Hedera. Describe a dapp, hh builds it, deploys it to testnet, and a harness decides whether it works.

<img alt="hh" src="demo/assets/hh.gif" width="600" />

The above example was recorded with [VHS](https://github.com/charmbracelet/vhs) ([view source](./demo/hero.tape)).

## Tutorial

Create a dapp project. It is [scaffold-hbar](https://github.com/hedera-dev/scaffold-hbar) with an `hh.yaml` and the official [hedera skills](https://github.com/hedera-dev/hedera-skills) for claude.

```sh
hh init --scaffold-hbar my-dapp --install
cd my-dapp && cp .env.example .env    # a testnet account from portal.hedera.com
```

Open hh and describe what you want.

```sh
hh
```

```
build a page where I can swap HBAR for USDC at a fixed rate of 1 HBAR = 0.05 USDC.
deploy a USDC test token the swap contract can mint, add a faucet button,
and show my HBAR and USDC balances
```

Claude writes the contracts, deploy scripts and the page. It has no keys. After every attempt hh compiles, type checks and deploys to testnet with the connected wallet (ctrl+w), and sends any failure back into the same conversation until it passes. Then:

```sh
yarn next:start    # http://localhost:3000
```

## Scenarios

Under the agent is a harness you can use on its own: yaml scenarios that run real hedera transactions and check the result on the mirror node, in code.

```yaml
actors:
  issuer: { hbar: 20 }
  alice: { key: ed25519 }

steps:
  - token.create: { as: gold, name: Gold, symbol: GLD, initial_supply: 1000, treasury: issuer, kyc_key: issuer }
  - token.associate: { account: alice, token: gold }
  - token.transfer: { token: gold, from: issuer, to: alice, amount: 5, expect: ACCOUNT_KYC_NOT_GRANTED_FOR_TOKEN }

assert:
  - token.balance: { account: alice, token: gold, equals: 0 }
```

```sh
hh run examples/               # on an in process mock network, milliseconds, no keys
hh run examples/ -n testnet    # the same files on real testnet
```

<img alt="hh run" src="demo/assets/run.gif" width="600" />

All seven [examples](./examples) pass on testnet ([results and hashscan links](./docs/guide.md#verified-on-testnet)). Tokens with kyc, freeze, pause, airdrops, nfts and custom fees, topics with running hash verification, and multisig schedules.

## Installation

```sh
# macOS apple silicon, or darwin_amd64, linux_amd64, linux_arm64, windows_amd64.zip
curl -sL https://github.com/Gmin2/hedera-harness-go/releases/latest/download/hh_darwin_arm64.tar.gz | tar xz

# or with go 1.26
go install github.com/Gmin2/hedera-harness-go@latest
```

Agent mode uses the [claude cli](https://claude.com/claude-code) with your existing login.

## Commands

| | |
|---|---|
| `hh` | the tui: prompts, `/judge`, `/check`, ctrl+w wallet, ctrl+r scenarios, ctrl+n network |
| `hh init [--scaffold-hbar]` | start a project or a dapp |
| `hh agent <prompt>` | the same loop from the cli |
| `hh run`, `hh check` | run or validate scenarios |
| `hh wallet`, `hh doctor` | check the account before spending anything |

Configuration, networks, wallets and every step and assertion are in the [guide](./docs/guide.md).

## Compared to hedera-harness

hh takes direct inspiration from [hedera-dev/hedera-harness](https://github.com/hedera-dev/hedera-harness): the harness decides pass or fail, never the agent.

| | hedera-harness | hh |
|---|---|---|
| start a feature | write a recipe: spec, prd, validators, checklist | one sentence |
| agent | unattended batch run | a streaming conversation |
| chain checks | an llm reads curl output | scenarios evaluated in code |
| networks | testnet | mock, solo, testnet |
| language | typescript | go |

## License

[Apache-2.0](LICENSE)
