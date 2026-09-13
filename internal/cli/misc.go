package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/assert"
	"github.com/Gmin2/hedera-harness-go/internal/mock"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
)

func opsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ops",
		Short: "List the steps and assertions a scenario can use",
		Run: func(cmd *cobra.Command, args []string) {
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "steps:")
			for _, n := range ops.Names() {
				fmt.Fprintf(w, "  %-20s %s\n", n, strings.Join(ops.Fields(n), ", "))
			}
			fmt.Fprintln(w, "\nassertions:")
			for _, n := range assert.Names() {
				fmt.Fprintf(w, "  %-20s %s\n", n, strings.Join(assert.Fields(n), ", "))
			}
		},
	}
}

func mockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mock",
		Short: "Start the mock network and keep it running for other tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := mock.Start(mock.Options{})
			if err != nil {
				return err
			}
			defer srv.Close()
			id, key := srv.Operator()
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "consensus    %s  (node 0.0.3)\n", srv.ConsensusAddr())
			fmt.Fprintf(w, "mirror grpc  %s\n", srv.MirrorGRPCAddr())
			fmt.Fprintf(w, "mirror rest  %s\n", srv.MirrorRESTURL())
			fmt.Fprintf(w, "operator     %s\n", id)
			fmt.Fprintf(w, "operator key %s\n\n", key.StringDer())
			fmt.Fprintln(w, "ctrl+c to stop")

			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt)
			select {
			case <-stop:
			case <-cmd.Context().Done():
			}
			return nil
		},
	}
}
