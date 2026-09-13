// Command hh-tui-demo runs the hh tui against a scripted fake runner. Any
// text that is not a command plays a fake agent turn, and follow up prompts
// continue the same fake conversation. It starts as if an hh.yaml had
// preloaded a judge and a check, /judge off and /check off drop them. ctrl+w
// opens a fake connect wallet dialog.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui"
	"github.com/Gmin2/hedera-harness-go/internal/tui/demo"
)

func main() {
	network := flag.String("network", "mock", "network to start on: mock, local or testnet")
	noAnim := flag.Bool("no-anim", false, "freeze spinners")
	flag.Parse()

	cwd, _ := os.Getwd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := tui.Run(ctx, tui.Options{
		Version:  "v0.1.0-demo",
		Cwd:      cwd,
		Network:  *network,
		Networks: []string{"mock", "local", "testnet"},
		Operator: "0.0.2",
		NoAnim:   *noAnim,
		Scenarios: []tui.Scenario{
			{Name: "token-flow", Path: "scenarios/token-flow.yaml", Description: "create, associate and move a fungible token", Steps: 8, Assertions: 6},
			{Name: "topic-submit", Path: "scenarios/topic-submit.yaml", Description: "create a topic and submit messages", Steps: 4, Assertions: 3},
			{Name: "scheduled-payout", Path: "scenarios/scheduled-payout.yaml", Description: "multi sig scheduled hbar transfer", Steps: 6, Assertions: 4},
			{Name: "nft-mint", Path: "scenarios/nft-mint.yaml", Description: "mint and transfer an nft collection", Steps: 5, Assertions: 5},
		},
		Launch: demo.Launch,
		Agent: func(ctx context.Context, req tui.AgentRequest, sink event.Sink) error {
			return demo.Agent(ctx, demoRequest(req), sink)
		},
		AgentName: "claude",
		Judges:    []string{"scenarios/scheduled-payout.yaml"},
		Checks:    []tui.Check{{Name: "go test ./...", Run: "go test ./..."}},
		Project:   filepath.Join(cwd, "hh.yaml"),
		Wallet:    tui.Wallet(demo.DefaultWallet(*network)),
		Wallets: func(ctx context.Context, network string) ([]tui.Wallet, error) {
			list, err := demo.Wallets(ctx, network)
			out := make([]tui.Wallet, len(list))
			for i, w := range list {
				out[i] = tui.Wallet(w)
			}
			return out, err
		},
		ConnectWallet: func(ctx context.Context, network string, choice tui.WalletChoice) (tui.Wallet, error) {
			w, err := demo.ConnectWallet(ctx, network, demo.WalletChoice(choice))
			return tui.Wallet(w), err
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func demoRequest(req tui.AgentRequest) demo.Request {
	out := demo.Request{Prompt: req.Prompt, Network: req.Network, SessionID: req.SessionID, Judges: req.Judges}
	for _, c := range req.Checks {
		out.Checks = append(out.Checks, demo.Check(c))
	}
	return out
}
