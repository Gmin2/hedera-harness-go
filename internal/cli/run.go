package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/report"
	"github.com/Gmin2/hedera-harness-go/internal/runner"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

var errFailed = errors.New("scenario failed")

func runCmd() *cobra.Command {
	var (
		asJSON    bool
		keepGoing bool
		timeout   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run [scenario.yaml|dir]...",
		Short: "Run scenarios and check their assertions (default: the judges in hh.yaml)",
		RunE: func(cmd *cobra.Command, args []string) error {
			scenarios, err := collect(defaultScenarios(args))
			if err != nil {
				return err
			}
			width := 100
			if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil {
				width = w
			}

			var reports []*runner.Report
			failed := false
			for _, sc := range scenarios {
				mode, err := networkFlag(cmd, sc.Network)
				if err != nil {
					return err
				}
				t, closeTarget, err := open(cmd.Context(), mode)
				if err != nil {
					return err
				}
				opts := runner.Defaults(mode)
				opts.KeepGoing = keepGoing
				if timeout > 0 {
					opts.Poll.Timeout = timeout
				}
				var sink event.Sink
				if !asJSON {
					sink = report.NewPlain(cmd.OutOrStdout(), width).Sink
				}
				rep := runner.Run(cmd.Context(), t, sc, opts, sink)
				closeTarget()
				reports = append(reports, rep)
				if rep.Status != event.Passed {
					failed = true
				}
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				var out any = reports
				if len(reports) == 1 {
					out = reports[0]
				}
				if err := enc.Encode(out); err != nil {
					return err
				}
			}
			if failed {
				cmd.SilenceErrors = true
				return errFailed
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as json instead of the live view")
	cmd.Flags().BoolVar(&keepGoing, "keep-going", false, "keep running steps after an unexpected result")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long each assertion may poll the mirror node")
	return cmd
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [scenario.yaml|dir]...",
		Short: "Validate scenarios without touching a network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && proj != nil && len(proj.Scenarios) > 0 {
				args = proj.Scenarios
			}
			scenarios, err := collect(defaultScenarios(args))
			if err != nil {
				return err
			}
			bad := 0
			for _, sc := range scenarios {
				if err := runner.Plan(sc); err != nil {
					bad++
					fmt.Fprintf(cmd.OutOrStdout(), "× %s\n", sc.Path)
					for _, line := range strings.Split(err.Error(), "\n") {
						fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", line)
					}
					continue
				}
				steps, assertions := runner.Counts(sc)
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s  %d actors · %d steps · %d assertions\n", sc.Path, len(sc.Actors), steps, assertions)
			}
			if bad > 0 {
				cmd.SilenceErrors = true
				return fmt.Errorf("%d invalid scenario(s)", bad)
			}
			return nil
		},
	}
}

// defaultScenarios falls back to hh.yaml judges, then its scenario dirs.
func defaultScenarios(args []string) []string {
	if len(args) > 0 || proj == nil {
		return args
	}
	if len(proj.Judge.Scenarios) > 0 {
		return proj.Judge.Scenarios
	}
	return proj.Scenarios
}

// collect loads files and every scenario under directories.
func collect(args []string) ([]*scenario.Scenario, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("give a scenario file or directory, or run hh init to create hh.yaml")
	}
	var out []*scenario.Scenario
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			found, err := scenario.Discover(a)
			if err != nil {
				return nil, err
			}
			if len(found) == 0 {
				return nil, fmt.Errorf("no scenarios found in %s", a)
			}
			out = append(out, found...)
			continue
		}
		sc, err := scenario.Load(filepath.Clean(a))
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, nil
}
