package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/target"
)

// trimmed from a real `claude -p --output-format stream-json --verbose` run
const sampleStream = `{"type":"system","subtype":"init","session_id":"s-1","model":"claude-haiku-4-5-20251001","tools":["Bash"]}
{"type":"assistant","message":{"model":"claude-haiku-4-5-20251001","content":[{"type":"thinking","thinking":""}]},"session_id":"s-1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hello-from-hh","description":"say hi"}}]},"session_id":"s-1"}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"hello-from-hh","is_error":false}]},"session_id":"s-1"}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"}],"is_error":true}]}}
not json, ignored
{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]},"session_id":"s-1"}
{"type":"result","subtype":"success","is_error":false,"num_turns":2,"result":"done","session_id":"s-1","total_cost_usd":0.0258}
`

func TestStreamParser(t *testing.T) {
	var events []event.Event
	p := &streamParser{attempt: 1, sink: func(e event.Event) { events = append(events, e) }, tools: map[string]string{}}
	if err := p.read(strings.NewReader(sampleStream)); err != nil {
		t.Fatal(err)
	}
	if !p.sawResult || p.failure != "" {
		t.Fatalf("result: saw=%v failure=%q", p.sawResult, p.failure)
	}
	r := p.result
	if r.SessionID != "s-1" || r.Model != "claude-haiku-4-5-20251001" || r.Turns != 2 || r.CostUSD != 0.0258 || r.Text != "done" {
		t.Fatalf("result: %+v", r)
	}
	if len(events) != 4 {
		t.Fatalf("want 4 events, got %d: %#v", len(events), events)
	}
	tool, ok := events[0].(event.AgentTool)
	if !ok || tool.Name != "Bash" || tool.Summary != "echo hello-from-hh" {
		t.Fatalf("tool: %#v", events[0])
	}
	if res := events[1].(event.AgentToolResult); res.Name != "Bash" || res.Output != "hello-from-hh" || res.IsError {
		t.Fatalf("tool result: %#v", res)
	}
	if res := events[2].(event.AgentToolResult); res.Output != "line one\nline two" || !res.IsError {
		t.Fatalf("block tool result: %#v", res)
	}
	if txt := events[3].(event.AgentText); txt.Text != "done" {
		t.Fatalf("text: %#v", txt)
	}
}

func TestStreamParserErrorResult(t *testing.T) {
	p := &streamParser{sink: func(event.Event) {}, tools: map[string]string{}}
	_ = p.read(strings.NewReader(`{"type":"result","subtype":"error_max_turns","is_error":true,"session_id":"s-2"}` + "\n"))
	if !strings.Contains(p.failure, "error_max_turns") || p.result.SessionID != "s-2" {
		t.Fatalf("failure %q result %+v", p.failure, p.result)
	}
}

func TestClaudeArgs(t *testing.T) {
	args := Claude{}.args(Request{Prompt: "fix it", Model: "sonnet", SessionID: "s-9", SystemPrompt: "sys"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p fix it", "--output-format stream-json", "--verbose", "--resume s-9", "--model sonnet", "--append-system-prompt sys", "--allowedTools Bash,Read,Edit,Write,Glob,Grep"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
}

// fakeAgent writes the judge scenario on each attempt, broken the first time.
type fakeAgent struct {
	dir     string
	writes  []string
	prompts []string
	session []string
}

func (f *fakeAgent) Run(ctx context.Context, req Request, attempt int, sink event.Sink) (Result, error) {
	f.prompts = append(f.prompts, req.Prompt)
	f.session = append(f.session, req.SessionID)
	sink(event.AgentTool{Attempt: attempt, ID: "w", Name: "Write", Summary: "transfer.yaml"})
	body := f.writes[min(attempt-1, len(f.writes)-1)]
	if err := os.WriteFile(filepath.Join(f.dir, "transfer.yaml"), []byte(body), 0o644); err != nil {
		return Result{}, err
	}
	sink(event.AgentToolResult{Attempt: attempt, ID: "w", Name: "Write", Output: "ok"})
	return Result{SessionID: "session-1", CostUSD: 0.01, Turns: 2}, nil
}

func TestLoopRepairsUntilJudgePasses(t *testing.T) {
	dir := t.TempDir()
	agent := &fakeAgent{dir: dir, writes: []string{
		// wrong expectation, bob ends with 11 hbar
		"actors:\n  alice: {}\n  bob: {}\nsteps:\n  - hbar.transfer: { from: alice, to: bob, amount: 1 }\nassert:\n  - account.hbar: { account: bob, equals: 99 }\n",
		"actors:\n  alice: {}\n  bob: {}\nsteps:\n  - hbar.transfer: { from: alice, to: bob, amount: 1 }\nassert:\n  - account.hbar: { account: bob, equals: 11 }\n",
	}}

	var events []event.Event
	err := Loop(context.Background(), Options{
		Prompt:      "write a transfer scenario",
		Dir:         dir,
		Judges:      []string{"transfer.yaml"},
		Network:     network.Mock,
		MaxAttempts: 3,
		HH:          "hh",
		Agent:       agent,
		Open:        target.Open,
	}, func(e event.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.prompts) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(agent.prompts))
	}
	if !strings.Contains(agent.prompts[1], "expected = 99 ℏ, actual 11 ℏ") {
		t.Fatalf("repair prompt should carry the finding:\n%s", agent.prompts[1])
	}
	if agent.session[0] != "" || agent.session[1] != "session-1" {
		t.Fatalf("repair should resume the session, got %v", agent.session)
	}

	var judges []event.JudgeFinished
	var last event.LoopFinished
	for _, e := range events {
		switch e := e.(type) {
		case event.JudgeFinished:
			judges = append(judges, e)
		case event.LoopFinished:
			last = e
		}
	}
	if len(judges) != 2 || judges[0].Status != event.Failed || judges[1].Status != event.Passed {
		t.Fatalf("judges: %+v", judges)
	}
	if last.Status != event.Passed || last.Attempts != 2 || last.CostUSD != 0.02 {
		t.Fatalf("loop finished: %+v", last)
	}
}

func TestLoopGivesUp(t *testing.T) {
	dir := t.TempDir()
	agent := &fakeAgent{dir: dir, writes: []string{"name: nothing to see\n"}}
	var last event.LoopFinished
	err := Loop(context.Background(), Options{
		Prompt: "x", Dir: dir, Judges: []string{"transfer.yaml"}, Network: network.Mock,
		MaxAttempts: 2, Agent: agent, Open: target.Open,
	}, func(e event.Event) {
		if lf, ok := e.(event.LoopFinished); ok {
			last = lf
		}
	})
	if err == nil || last.Status != event.Failed || last.Attempts != 2 {
		t.Fatalf("err %v, last %+v", err, last)
	}
	if !strings.Contains(agent.prompts[1], "no steps and no assert") {
		t.Fatalf("invalid scenario should come back as a finding:\n%s", agent.prompts[1])
	}
}

func TestSystemPromptListsEveryOp(t *testing.T) {
	p := SystemPrompt("/bin/hh", []string{"a.yaml"}, "mock")
	for _, want := range []string{"token.airdrop:", "schedule.executed:", "/bin/hh run <file.yaml>", "- a.yaml"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
