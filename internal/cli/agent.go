package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/agent"
	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/report"
)

func agentCmd() *cobra.Command {
	var (
		judges      []string
		checks      []string
		dir         string
		model       string
		maxAttempts int
		asJSON      bool
		claudeBin   string
		resume      string
		maxCost     float64
		timeout     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "agent <prompt>",
		Short: "Let claude code do a task and judge the result with hh scenarios",
		Long: `hh agent runs the claude cli on a prompt, streams what it does, then runs the
judge scenarios. When a judge fails, the failing steps and assertions go back to
the same claude session as a repair prompt, until the judge passes or attempts
run out. Uses your existing claude code login.`,
		Example: `  hh agent "write examples/stablecoin.yaml: a token with kyc and a 1% fractional fee" --judge examples/stablecoin.yaml
  hh agent "fix the airdrop scenario" --judge examples/03-airdrop.yaml --max-attempts 2
  echo "add a scheduled payout scenario" | hh agent --judge payout.yaml
  hh agent --resume 53803b19-... "now add a pause step"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if prompt == "" && !term.IsTerminal(os.Stdin.Fd()) {
				b, _ := io.ReadAll(os.Stdin)
				prompt = strings.TrimSpace(string(b))
			}
			if prompt == "" {
				return errors.New("give the agent a prompt: hh agent \"...\"")
			}
			mode, err := networkFlag(cmd, "")
			if err != nil {
				return err
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("model") && os.Getenv("HH_AGENT_MODEL") != "" {
				model = os.Getenv("HH_AGENT_MODEL")
				cmd.Flags().Set("model", model)
			}
			if !cmd.Flags().Changed("max-cost") && os.Getenv("HH_AGENT_MAX_COST") != "" {
				cmd.Flags().Set("max-cost", os.Getenv("HH_AGENT_MAX_COST"))
			}

			// hh.yaml fills whatever the flags leave out
			loopChecks := projectChecks()
			if len(checks) > 0 {
				loopChecks = nil
				for _, c := range checks {
					loopChecks = append(loopChecks, agent.Check{Run: c})
				}
			}
			if proj != nil {
				flags := cmd.Flags()
				if !flags.Changed("judge") {
					judges = proj.Judge.Scenarios
				}
				if !flags.Changed("model") && proj.Agent.Model != "" {
					model = proj.Agent.Model
				}
				if !flags.Changed("max-cost") && proj.Agent.MaxCost > 0 {
					maxCost = proj.Agent.MaxCost
				}
				if !flags.Changed("max-attempts") && proj.Agent.MaxAttempts > 0 {
					maxAttempts = proj.Agent.MaxAttempts
				}
				if !flags.Changed("timeout") && proj.Agent.Timeout.Duration > 0 {
					timeout = proj.Agent.Timeout.Duration
				}
			}

			var sink event.Sink
			if asJSON {
				sink = jsonLines(cmd.OutOrStdout())
			} else {
				width := 100
				if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil {
					width = w
				}
				sink = report.NewPlain(cmd.OutOrStdout(), width).Sink
			}

			err = agent.Loop(cmd.Context(), agent.Options{
				Prompt:         prompt,
				Dir:            absDir,
				Judges:         judges,
				Checks:         loopChecks,
				Network:        mode,
				MaxAttempts:    maxAttempts,
				Model:          model,
				HH:             hhPath(),
				Agent:          agent.Claude{Command: claudeBin},
				Open:           open,
				CheckEnv:       checkEnv(mode),
				SessionID:      resume,
				MaxCostUSD:     maxCost,
				AttemptTimeout: timeout,
				Stream:         !asJSON,
			}, sink)
			if err != nil {
				cmd.SilenceErrors = true
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&judges, "judge", "j", nil, "scenario that must pass, repeatable (default from hh.yaml)")
	cmd.Flags().StringArrayVar(&checks, "check", nil, "shell command that must exit 0, repeatable (default from hh.yaml)")
	cmd.Flags().StringVar(&dir, "dir", ".", "directory the agent works in")
	cmd.Flags().StringVarP(&model, "model", "m", "", "claude model, eg sonnet or opus (default: your claude code default)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 3, "agent attempts including repairs")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print events as json lines")
	cmd.Flags().StringVar(&claudeBin, "claude", "claude", "path to the claude cli")
	cmd.Flags().StringVar(&resume, "resume", "", "continue an earlier conversation by session id")
	cmd.Flags().Float64Var(&maxCost, "max-cost", 0, "stop once the attempts cost this many usd (0 = no cap)")
	cmd.Flags().DurationVar(&timeout, "timeout", 20*time.Minute, "longest one agent attempt may run")
	return cmd
}

// hhPath is how the agent should call hh: this binary when it is a real
// install, otherwise plain hh on PATH (go run builds into a temp dir).
func hhPath() string {
	exe, err := os.Executable()
	if err != nil || strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return "hh"
	}
	return exe
}

func jsonLines(w io.Writer) event.Sink {
	var mu sync.Mutex
	enc := json.NewEncoder(w)
	return func(e event.Event) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(struct {
			Type  string      `json:"type"`
			Event event.Event `json:"event"`
		}{fmt.Sprintf("%T", e)[len("event."):], e})
	}
}
