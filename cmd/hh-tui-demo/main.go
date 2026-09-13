// Command hh-tui-demo runs the hh tui against a scripted fake runner. Any
// text that is not a command plays a fake agent turn, and follow up prompts
// continue the same fake conversation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

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
			return demo.Agent(ctx, demo.Request(req), sink)
		},
		AgentName: "claude",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
