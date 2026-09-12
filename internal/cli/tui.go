package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

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
	target, closeTarget, err := openTarget(ctx, mode)
	if err != nil {
		return err
	}
	defer closeTarget()
	rep := runner.Run(ctx, target, sc, runner.Defaults(mode), sink)
	if rep.Error != "" && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
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
