// Package cli wires the hh commands.
package cli

import (
	"context"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/agent"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/project"
)

var Version = "0.1.0-dev"

// proj is the hh.yaml in the working directory, nil when there is none.
var proj *project.Config

func Execute() int {
	_ = network.LoadDotEnv(".env")

	root := &cobra.Command{
		Use:   "hh",
		Short: "A Hedera harness: scenarios, assertions and a local mock network",
		Long: `hh runs YAML scenarios against Hedera and checks the result on the mirror node.

Networks:
  mock     an in process fake hedera, no docker, no keys, milliseconds per step
  local    solo (or hiero local node with HH_LOCAL_PROFILE=localnode)
  testnet  real testnet, needs HEDERA_OPERATOR_ID and HEDERA_OPERATOR_KEY`,
		Example: `  hh run examples/token-kyc.yaml
  hh run examples/ --network testnet
  hh check examples/
  hh doctor --network testnet`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			proj, err = project.Load(".")
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd)
		},
	}
	root.PersistentFlags().StringP("network", "n", "", "mock, local or testnet (default from the scenario, else mock)")

	root.AddCommand(initCmd(), runCmd(), checkCmd(), agentCmd(), doctorCmd(), opsCmd(), mockCmd())

	if err := fang.Execute(context.Background(), root, fang.WithVersion(Version), fang.WithNotifySignal(os.Interrupt)); err != nil {
		return 1
	}
	return 0
}

// networkFlag picks the network: the flag, then HH_NETWORK, then the
// scenario, then hh.yaml, then mock.
func networkFlag(cmd *cobra.Command, fallback string) (network.Mode, error) {
	v, _ := cmd.Flags().GetString("network")
	if v == "" {
		v = os.Getenv("HH_NETWORK")
	}
	if v == "" {
		v = fallback
	}
	if v == "" && proj != nil {
		v = proj.Network
	}
	return network.ParseMode(v)
}

// projectChecks converts hh.yaml checks for the agent loop.
func projectChecks() []agent.Check {
	if proj == nil {
		return nil
	}
	var out []agent.Check
	for _, c := range proj.Judge.Checks {
		out = append(out, agent.Check{Name: c.Name, Run: c.Run, Timeout: c.Timeout.Duration})
	}
	return out
}
