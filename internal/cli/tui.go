package cli

import (
	"context"
	"fmt"
	"github.com/Gmin2/hedera-harness-go/internal/target"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/agent"
	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/runner"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
	"github.com/Gmin2/hedera-harness-go/internal/tui"
)

// runTUI starts the interactive ui over the scenarios found under the
// current directory. Without a terminal it prints help instead.
func runTUI(cmd *cobra.Command) error {
	if !term.IsTerminal(os.Stdout.Fd()) {
		return cmd.Help()
	}
	mode, err := networkFlag(cmd, "")
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()

	found, _ := scenario.Discover(".")
	var scenarios []tui.Scenario
	for _, sc := range found {
		steps, assertions := runner.Counts(sc)
		scenarios = append(scenarios, tui.Scenario{
			Name:        sc.Name,
			Path:        sc.Path,
			Description: sc.Description,
			Steps:       steps,
			Assertions:  assertions,
		})
	}

	var networks []string
	for _, m := range network.Modes {
		networks = append(networks, string(m))
	}

	return tui.Run(cmd.Context(), tui.Options{
		Version:   Version,
		Cwd:       home(cwd),
		Network:   string(mode),
		Networks:  networks,
		Operator:  operatorHint(mode),
		Scenarios: scenarios,
		NoAnim:    os.Getenv("HH_NO_ANIM") != "",
		Launch:    launch,
		Agent:     runAgent(cwd),
		AgentName: "claude",
	})
}

func launch(ctx context.Context, path, net string, sink event.Sink) error {
	sc, err := scenario.Load(path)
	if err != nil {
		return err
	}
	mode, err := network.ParseMode(net)
	if err != nil {
		return err
	}
	t, closeTarget, err := target.Open(ctx, mode)
	if err != nil {
		return err
	}
	defer closeTarget()
	rep := runner.Run(ctx, t, sc, runner.Defaults(mode), sink)
	if rep.Error != "" && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// runAgent runs one prompt of a tui conversation, judged on the selected network.
func runAgent(dir string) func(ctx context.Context, req tui.AgentRequest, sink event.Sink) error {
	turns := 0
	return func(ctx context.Context, req tui.AgentRequest, sink event.Sink) error {
		mode, err := network.ParseMode(req.Network)
		if err != nil {
			return err
		}
		if req.SessionID == "" {
			turns = 0
		}
		turns++
		var maxCost float64
		if v := os.Getenv("HH_AGENT_MAX_COST"); v != "" {
			fmt.Sscanf(v, "%g", &maxCost)
		}
		return agent.Loop(ctx, agent.Options{
			Prompt:         req.Prompt,
			Dir:            dir,
			Judges:         req.Judges,
			Network:        mode,
			Model:          os.Getenv("HH_AGENT_MODEL"),
			HH:             hhPath(),
			Agent:          agent.Claude{},
			Open:           target.Open,
			SessionID:      req.SessionID,
			Turn:           turns,
			MaxCostUSD:     maxCost,
			AttemptTimeout: 20 * time.Minute,
			Stream:         true,
		}, sink)
	}
}

func operatorHint(mode network.Mode) string {
	if mode == network.Mock {
		return "0.0.2"
	}
	cfg, err := network.FromEnv(mode)
	if err != nil {
		return ""
	}
	return cfg.OperatorID
}

func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, h) {
		return filepath.Join("~", strings.TrimPrefix(path, h))
	}
	return path
}
