package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

// Check is a shell command the agent's work must pass, like a build or tests.
type Check struct {
	Name    string
	Run     string
	Timeout time.Duration
}

func (c Check) label() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Run
}

const (
	defaultCheckTimeout = 10 * time.Minute
	checkOutputLines    = 30
)

// runCheck runs one check in dir with a shell and reports the result.
func runCheck(ctx context.Context, dir string, c Check, attempt int, env []string, sink event.Sink) event.CheckFinished {
	sink(event.CheckStarted{Attempt: attempt, Name: c.label(), Command: c.Run})
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Run)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	setProcessGroup(cmd)
	var out tail
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	fin := event.CheckFinished{
		Attempt: attempt,
		Name:    c.label(),
		Command: c.Run,
		Status:  event.Passed,
		Output:  lastLines(out.String(), checkOutputLines),
		Elapsed: time.Since(start),
	}
	var exit *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		fin.Status, fin.ExitCode, fin.Error = event.Failed, -1, fmt.Sprintf("timed out after %s", timeout)
	case errors.As(err, &exit):
		fin.Status, fin.ExitCode = event.Failed, exit.ExitCode()
	case err != nil:
		fin.Status, fin.ExitCode, fin.Error = event.Failed, -1, err.Error()
	}
	sink(fin)
	return fin
}

// checkFinding explains a failed check in a way the agent can act on.
func checkFinding(f event.CheckFinished) string {
	why := fmt.Sprintf("exited %d", f.ExitCode)
	if f.Error != "" {
		why = f.Error
	}
	s := fmt.Sprintf("check %q (`%s`) failed: %s", f.Name, f.Command, why)
	if out := strings.TrimSpace(f.Output); out != "" {
		s += "\n  output:\n    " + strings.ReplaceAll(out, "\n", "\n    ")
	}
	return s
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("… %d earlier lines", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
