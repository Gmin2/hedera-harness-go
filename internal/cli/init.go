package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

const scaffoldConfig = `# hh on a scaffold-hbar dapp: type what to build, claude builds it, hh deploys and judges.
network: testnet
# the connected wallet deploys the contracts. default is your portal account from .env,
# burner creates a fresh funded account per run: wallet: { kind: burner, fund: 120 }
wallet: default

agent:
  model: sonnet
  max_cost: 5
  max_attempts: 3
  timeout: 30m

# run after every attempt, failures go back to claude
judge:
  checks:
    - { name: compile, run: yarn hardhat:compile, timeout: 5m }
    - { name: frontend types, run: yarn next:check-types, timeout: 5m }
    - { name: deploy to testnet, run: cd packages/hardhat && npx hardhat deploy --network hederaTestnet --reset, timeout: 10m }
  # optional on-chain checks with hh scenarios, eg .hh/scenarios/hedera-token.yaml
  scenarios: []
`

const scaffoldScenario = `name: hedera token deployed
description: optional on-chain check, reads the HederaToken the deploy check put on testnet

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
		scaffold   bool
		from       string
		skillsFrom string
		install    bool
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
				if len(args) == 0 {
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
				if err := ignoreEnv(dir); err != nil {
					return err
				}
				if err := installSkills(cmd, skillsFrom, dir); err != nil {
					fmt.Fprintf(w, "  skipped hedera skills: %v\n", err)
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
				fmt.Fprintln(w, "        hh                 type what to build, eg: build a page where I can swap HBAR for USDC")
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
	cmd.Flags().StringVar(&skillsFrom, "skills-from", "https://github.com/hedera-dev/hedera-skills.git", "git url or local path of hedera-skills, copied into .claude/skills")
	return cmd
}

// ignoreEnv makes sure the operator key in .env never gets committed.
func ignoreEnv(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	b, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == ".env" {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n.env\n")
	return err
}

// hedera skills claude code loads from .claude/skills when building the dapp
var dappSkills = []string{
	"native-services-js/skills/hedera-token-service",
	"native-services-js/skills/hedera-consensus-service",
	"system-contracts/skills/hts-system-contract",
	"system-contracts/skills/hss-system-contract",
}

func installSkills(cmd *cobra.Command, from, dir string) error {
	src := from
	if _, err := os.Stat(filepath.Join(from, "plugins")); err != nil {
		tmp, err := os.MkdirTemp("", "hh-skills-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		git := exec.CommandContext(cmd.Context(), "git", "clone", "--depth", "1", "-q", from, tmp)
		if err := git.Run(); err != nil {
			return fmt.Errorf("git clone %s: %w", from, err)
		}
		src = tmp
	}
	for _, rel := range dappSkills {
		from := filepath.Join(src, "plugins", rel)
		to := filepath.Join(dir, ".claude", "skills", filepath.Base(rel))
		if err := os.CopyFS(to, os.DirFS(from)); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  added   .claude/skills (%d hedera skills)\n", len(dappSkills))
	return nil
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
