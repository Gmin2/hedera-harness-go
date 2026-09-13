package cli

import (
	"context"
	"fmt"
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
	"github.com/Gmin2/hedera-harness-go/internal/wallet"
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

	var found []*scenario.Scenario
	roots := []string{"."}
	if proj != nil && len(proj.Scenarios) > 0 {
		roots = proj.Scenarios
	}
	seen := map[string]bool{}
	for _, root := range roots {
		list, _ := collect([]string{root})
		for _, sc := range list {
			if !seen[sc.Path] {
				seen[sc.Path] = true
				found = append(found, sc)
			}
		}
	}
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

	opts := tui.Options{
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

		Wallets:       tuiWallets,
		ConnectWallet: tuiConnect,
	}
	if info, err := wallet.Connect(cmd.Context(), mode, currentWallet()); err == nil || info.Label != "" {
		opts.Wallet = toTUIWallet(info)
	}
	if proj != nil {
		opts.Project = proj.Path
		opts.Judges = proj.Judge.Scenarios
		for _, c := range proj.Judge.Checks {
			opts.Checks = append(opts.Checks, tui.Check{Name: c.Name, Run: c.Run})
		}
	}
	return tui.Run(cmd.Context(), opts)
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
	t, closeTarget, err := open(ctx, mode)
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

// runAgent runs one prompt of a tui conversation, judged on the selected
// network. Settings come from hh.yaml, HH_AGENT_* variables override them.
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

		o := agent.Options{
			Prompt:         req.Prompt,
			Dir:            dir,
			Judges:         req.Judges,
			Network:        mode,
			HH:             hhPath(),
			Agent:          agent.Claude{},
			Open:           open,
			CheckEnv:       checkEnv(mode),
			SessionID:      req.SessionID,
			Turn:           turns,
			AttemptTimeout: 20 * time.Minute,
			Stream:         true,
		}
		for _, c := range req.Checks {
			timeout := time.Duration(0)
			for _, pc := range projectChecks() {
				if pc.Run == c.Run {
					timeout = pc.Timeout
				}
			}
			o.Checks = append(o.Checks, agent.Check{Name: c.Name, Run: c.Run, Timeout: timeout})
		}
		if proj != nil {
			o.Model = proj.Agent.Model
			o.MaxCostUSD = proj.Agent.MaxCost
			o.MaxAttempts = proj.Agent.MaxAttempts
			if proj.Agent.Timeout.Duration > 0 {
				o.AttemptTimeout = proj.Agent.Timeout.Duration
			}
		}
		if v := os.Getenv("HH_AGENT_MODEL"); v != "" {
			o.Model = v
		}
		if v := os.Getenv("HH_AGENT_MAX_COST"); v != "" {
			fmt.Sscanf(v, "%g", &o.MaxCostUSD)
		}
		return agent.Loop(ctx, o, sink)
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
