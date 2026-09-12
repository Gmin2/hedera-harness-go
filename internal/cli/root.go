// Package cli wires the hh commands.
package cli

import (
	"context"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/network"
)

var Version = "0.1.0-dev"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd)
		},
	}
	root.PersistentFlags().StringP("network", "n", "", "mock, local or testnet (default from the scenario, else mock)")

	root.AddCommand(runCmd(), checkCmd(), doctorCmd(), opsCmd(), mockCmd())

	if err := fang.Execute(context.Background(), root, fang.WithVersion(Version), fang.WithNotifySignal(os.Interrupt)); err != nil {
		return 1
	}
	return 0
}

func networkFlag(cmd *cobra.Command, fallback string) (network.Mode, error) {
	v, _ := cmd.Flags().GetString("network")
	if v == "" {
		v = os.Getenv("HH_NETWORK")
	}
	if v == "" {
		v = fallback
	}
	return network.ParseMode(v)
}
