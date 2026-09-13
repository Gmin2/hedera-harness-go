// Package agent drives a coding agent and judges its work with hh scenarios.
//
// Like hedera-dev/hedera-harness it does not talk to a model itself: it runs
// the claude cli in print mode, reads its stream-json output, and decides
// pass or fail with deterministic scenarios instead of trusting the agent.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

// Request is one agent turn: a fresh prompt, or a repair prompt that resumes
// the previous session.
type Request struct {
	Prompt       string
	Dir          string
	Model        string
	SessionID    string // resume this session when set
	SystemPrompt string
	MaxTurns     int
}

type Result struct {
	SessionID string
	Model     string
	Turns     int
	CostUSD   float64
	Text      string
	Elapsed   time.Duration
}

// Claude runs the claude cli. Command defaults to "claude".
type Claude struct {
	Command string
	Tools   []string
}

func (c Claude) command() string {
	if c.Command == "" {
		return "claude"
	}
	return c.Command
}

func (c Claude) args(req Request) []string {
	tools := c.Tools
	if len(tools) == 0 {
		tools = []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
	}
	args := []string{
		"-p", req.Prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "acceptEdits",
		"--allowedTools", strings.Join(tools, ","),
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprint(req.MaxTurns))
	}
	return args
}

// Run executes one request and streams what the agent does into sink.
func (c Claude) Run(ctx context.Context, req Request, attempt int, sink event.Sink) (Result, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, c.command(), c.args(req)...)
	cmd.Dir = req.Dir
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr tail
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Result{}, fmt.Errorf("%s not found, install claude code (https://claude.com/claude-code) and log in", c.command())
		}
		return Result{}, err
	}

	p := &streamParser{attempt: attempt, sink: sink, tools: map[string]string{}}
	parseErr := p.read(stdout)
	waitErr := cmd.Wait()

	res := p.result
	res.Elapsed = time.Since(start)
	switch {
	case ctx.Err() != nil:
		return res, ctx.Err()
	case parseErr != nil:
		return res, parseErr
	case p.failure != "":
		return res, errors.New(p.failure)
	case waitErr != nil:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return res, fmt.Errorf("%s exited: %s", c.command(), msg)
	case !p.sawResult:
		return res, fmt.Errorf("%s ended without a result", c.command())
	}
	return res, nil
}

// streamParser turns claude stream-json lines into events.
type streamParser struct {
	attempt   int
	sink      event.Sink
	tools     map[string]string // tool_use id to tool name
	result    Result
	sawResult bool
	failure   string
}

type streamLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Model     string          `json:"model"`
	Message   *streamMessage  `json:"message"`
	IsError   bool            `json:"is_error"`
	Result    string          `json:"result"`
	NumTurns  int             `json:"num_turns"`
	CostUSD   float64         `json:"total_cost_usd"`
	Errors    json.RawMessage `json:"errors"`
}

type streamMessage struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (p *streamParser) read(r io.Reader) error {
	sc := bufio.NewScanner(r)
	// tool results can put a whole file on one line
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var l streamLine
		if err := json.Unmarshal(line, &l); err != nil {
			continue
		}
		p.handle(l)
	}
	return sc.Err()
}

func (p *streamParser) handle(l streamLine) {
	if l.SessionID != "" {
		p.result.SessionID = l.SessionID
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" && l.Model != "" {
			p.result.Model = l.Model
		}
	case "assistant":
		if l.Message == nil {
			return
		}
		if l.Message.Model != "" {
			p.result.Model = l.Message.Model
		}
		for _, b := range l.Message.Content {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					p.sink(event.AgentText{Attempt: p.attempt, Text: b.Text})
				}
			case "tool_use":
				p.tools[b.ID] = b.Name
				p.sink(event.AgentTool{Attempt: p.attempt, ID: b.ID, Name: b.Name, Summary: summarizeInput(b.Name, b.Input)})
			}
		}
	case "user":
		if l.Message == nil {
			return
		}
		for _, b := range l.Message.Content {
			if b.Type != "tool_result" {
				continue
			}
			p.sink(event.AgentToolResult{
				Attempt: p.attempt,
				ID:      b.ToolUseID,
				Name:    p.tools[b.ToolUseID],
				Output:  resultText(b.Content),
				IsError: b.IsError,
			})
		}
	case "result":
		p.sawResult = true
		p.result.Turns = l.NumTurns
		p.result.CostUSD = l.CostUSD
		p.result.Text = l.Result
		if l.IsError || (l.Subtype != "" && l.Subtype != "success") {
			p.failure = "agent stopped: " + l.Subtype
			if l.Result != "" {
				p.failure += ": " + l.Result
			}
		}
	}
}

// summarizeInput picks the one field that says what a tool call does.
func summarizeInput(tool string, raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "pattern", "path", "url", "description", "prompt"} {
		if v, ok := in[k].(string); ok && v != "" {
			return firstLine(v)
		}
	}
	return ""
}

// resultText flattens a tool_result content, which is a string or a list of blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// tail keeps the last few kilobytes written to it, for error messages.
type tail struct{ buf []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }
