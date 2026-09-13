# demo video, under 5 minutes

tools: cutaway (screen, webcam, auto zoom, captions, export) for the live parts, vhs for the terminal clips.

## before recording

```sh
cd ~/coding/hack/eth/hedera/hedera-harness-go && git pull && go build -o hh .
vhs demo/compare.tape      # demo/out/compare.mp4, no network
vhs demo/proof.tape        # demo/out/proof.mp4, runs example 05 on mock then testnet
cutaway doctor
```

open http://localhost:3000 later from `~/coding/hack/eth/hedera/swap-dapp`. terminal at least 140x40, font about 18.

## scenes

| # | time | layout | what is on screen | what you say |
|---|---|---|---|---|
| 1 | 0:00 to 0:20 | talking head | you | "hh is claude code for hedera. you describe a dapp, it builds it, deploys it to testnet, and a harness decides if it works." |
| 2 | 0:20 to 0:50 | screen, vhs `compare.mp4` | official recipe files vs one prompt | "the official hedera harness needs a recipe of spec, prd and validators and runs unattended for up to two hours, testnet only, with an llm reading curl to judge the chain. hh starts from one sentence." |
| 3 | 0:50 to 1:10 | screen, cutaway | `hh` in swap-dapp: landing, ctrl+w wallet popup | "a connected wallet like a dapp, checked against the mirror node. this account deploys." |
| 4 | 1:10 to 2:40 | screen, cutaway, speed up the wait | type the prompt, claude streaming, checks compile, types, deploy passing, "dapp ready" | "claude writes the contracts, deploy scripts and the page using the official hedera skills. it has no keys. after each attempt hh compiles, type checks and deploys to testnet, and sends failures back into the same session." |
| 5 | 2:40 to 3:40 | screen, cutaway | browser: /swap, burner wallet, faucet, swap, balances, hashscan link | "the dapp works on testnet: swap hbar for usdc." |
| 6 | 3:40 to 4:25 | screen, vhs `proof.mp4` then README | same scenario on mock in ms and on testnet, verified on testnet table, ci badge | "under it is a go harness on hiero-sdk-go: yaml scenarios checked against the mirror node in code, an in process mock network, all 7 examples verified on testnet." |
| 7 | 4:25 to 4:45 | talking head | repo url | "github.com/Gmin2/hedera-harness-go, apache 2." |

## the prompt (scene 4)

```
build a page where I can swap HBAR for USDC at a fixed rate of 1 HBAR = 0.05 USDC. deploy a USDC test token the swap contract can mint, add a faucet button, and show my HBAR and USDC balances
```

## the wallet in the browser (scene 5)

devtools console on localhost:3000, once, then reload and pick Burner Wallet:

```js
localStorage.setItem("burnerWallet.pk", "<the 0x portal test key>")
```

## cutaway

```sh
cutaway record --out ~/demos/hh --keys --exclude com.brave.Browser   # exclude apps you do not want in shot, then cmd+shift+8 to start and stop
cutaway pitch --in ~/demos/hh --name "Mintu" --role "hh, claude code for hedera"
cutaway describe --in ~/demos/hh --json     # then trim dead air and add zooms in project.json or the timeline
cutaway export --in ~/demos/hh --out ~/demos/hh.mp4
```

drop `demo/out/compare.mp4` and `demo/out/proof.mp4` in at scenes 2 and 6, or play them full screen while cutaway records.
