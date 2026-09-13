package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/project"
)

const starterScenario = `name: first transfer
description: two fresh accounts and one hbar transfer, checked on the mirror node

actors:
  alice: {}
  bob: {}

steps:
  - hbar.transfer: { from: alice, to: bob, amount: 2.5 }

assert:
  - account.hbar: { account: bob, equals: 12.5 }
`

const envExample = `# testnet operator from portal.hedera.com (account id and DER private key)
HEDERA_OPERATOR_ID=
HEDERA_OPERATOR_KEY=
`

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [dir]",
		Short: "Create hh.yaml, a starter scenario and a .env template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			files := []struct{ path, body string }{
				{project.FileName, project.Template},
				{filepath.Join("scenarios", "first-transfer.yaml"), starterScenario},
				{".env.example", envExample},
			}
			w := cmd.OutOrStdout()
			for _, f := range files {
				created, err := writeNew(filepath.Join(dir, f.path), f.body)
				if err != nil {
					return err
				}
				if created {
					fmt.Fprintf(w, "  created %s\n", f.path)
				} else {
					fmt.Fprintf(w, "  kept    %s (already exists)\n", f.path)
				}
			}
			fmt.Fprintln(w, "\n  next: hh run    (judges on the mock network)")
			fmt.Fprintln(w, "        hh       (tui, type a prompt for claude)")
			return nil
		},
	}
}

// writeNew writes a file unless it already exists.
func writeNew(path, body string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(body), 0o644)
}
