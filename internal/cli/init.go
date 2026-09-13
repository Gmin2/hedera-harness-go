package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

const scaffoldConfig = `# hh on a scaffold-hbar dapp. flags override these.
network: testnet
# a fresh ecdsa account funded from your portal account for each run, swept back
# after. deploys sign with it through __RUNTIME_DEPLOYER_PRIVATE_KEY.
wallet: { kind: burner, fund: 50 }

scenarios: [.hh/scenarios]

agent:
  model: sonnet
  max_cost: 3
  max_attempts: 3
  timeout: 30m

# what claude's work must pass after every attempt, in this order
judge:
  checks:
    - { name: compile, run: yarn hardhat:compile, timeout: 5m }
    - { name: frontend types, run: yarn next:check-types, timeout: 5m }
    - { name: deploy, run: cd packages/hardhat && npx hardhat deploy --network hederaTestnet --reset, timeout: 10m }
  scenarios:
    - .hh/scenarios/hedera-token.yaml
`

const scaffoldScenario = `name: hedera token deployed
description: reads the HederaToken the deploy check just put on testnet

assert:
  - contract.call: { contract: HederaToken, function: "symbol()", returns: string, equals: HTK }
  - contract.call: { contract: HederaToken, function: "totalSupply()", equals: 10000e18 }
`

const envExample = `# testnet operator from portal.hedera.com (account id and DER private key)
HEDERA_OPERATOR_ID=
HEDERA_OPERATOR_KEY=
`

func initCmd() *cobra.Command {
	var (
		scaffold bool
		from     string
		install  bool
	)
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Create hh.yaml, a starter scenario and a .env template, or a whole scaffold-hbar dapp",
		Example: `  hh init
  hh init --scaffold-hbar my-dapp --install`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			w := cmd.OutOrStdout()

			files := []struct{ path, body string }{
				{project.FileName, project.Template},
				{filepath.Join("scenarios", "first-transfer.yaml"), starterScenario},
				{".env.example", envExample},
			}
			if scaffold {
				if dir == "." {
					dir = "scaffold-hbar"
				}
				if err := cloneScaffold(cmd, from, dir); err != nil {
					return err
				}
				files = []struct{ path, body string }{
					{project.FileName, scaffoldConfig},
					{filepath.Join(".hh", "scenarios", "hedera-token.yaml"), scaffoldScenario},
					{".env.example", envExample},
				}
			}
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

			if scaffold && install {
				fmt.Fprintln(w, "\n  yarn install (takes a few minutes the first time)")
				yarn := exec.CommandContext(cmd.Context(), "yarn", "install")
				yarn.Dir, yarn.Stdout, yarn.Stderr = dir, w, cmd.ErrOrStderr()
				if err := yarn.Run(); err != nil {
					return fmt.Errorf("yarn install: %w", err)
				}
			}

			if scaffold {
				fmt.Fprintf(w, "\n  next: cd %s\n", dir)
				if !install {
					fmt.Fprintln(w, "        yarn install")
				}
				fmt.Fprintln(w, "        cp .env.example .env   and fill in your portal account")
				fmt.Fprintln(w, "        hh wallet          check the account that funds the burner")
				fmt.Fprintln(w, "        hh                 ask claude for a feature, the checks and contract judge decide")
				return nil
			}
			fmt.Fprintln(w, "\n  next: hh run    (judges on the mock network)")
			fmt.Fprintln(w, "        hh       (tui, type a prompt for claude)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&scaffold, "scaffold-hbar", false, "clone the scaffold-hbar dapp template and set it up for hh")
	cmd.Flags().StringVar(&from, "from", "https://github.com/hedera-dev/scaffold-hbar.git", "git url or local path to clone scaffold-hbar from")
	cmd.Flags().BoolVar(&install, "install", false, "run yarn install after cloning")
	return cmd
}

func cloneScaffold(cmd *cobra.Command, from, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  kept    %s (already a project)\n", dir)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  cloning scaffold-hbar into %s\n", dir)
	git := exec.CommandContext(cmd.Context(), "git", "clone", "--depth", "1", from, dir)
	git.Stderr = cmd.ErrOrStderr()
	if err := git.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", from, err)
	}
	return nil
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
