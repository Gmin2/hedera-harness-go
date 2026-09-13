package run

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

// Judge is a scenario that checks the agent's work, with the status of its
// latest run. Status is empty until it has run once.
type Judge struct {
	Name   string
	Path   string
	Status event.Status
}

// Session is one agent prompt as seen by the ui: every attempt of the coding
// agent, the tools it called and the judge runs that checked the result.
type Session struct {
	sty    *styles.Styles
	static bool

	Prompt  string
	Agent   string
	Network string

	parts    []part
	pending  *pendingItem
	thinking *pendingItem
	tools    map[string]*toolItem
	runs     []*Run
	judges   []Judge

	attempt  int
	model    string
	turns    int
	cost     float64
	working  bool
	result   *event.LoopFinished
	canceled bool
	done     bool
}

// part is either a plain item or a judge run whose items are spliced in.
type part struct {
	item Item
	run  *Run
}

// NewSession starts a session for prompt. agent is the display name of the
// coding agent, judges the scenarios selected to check its work.
func NewSession(sty *styles.Styles, agent, prompt, network string, judges []Judge, static bool) *Session {
	s := &Session{
		sty:     sty,
		static:  static,
		Prompt:  prompt,
		Agent:   agent,
		Network: network,
		tools:   make(map[string]*toolItem),
		judges:  append([]Judge(nil), judges...),
	}
	s.add(&promptItem{sty: sty, text: prompt})
	s.pending = &pendingItem{sty: sty, name: agent, anim: newSpinner(sty, static, "agent", "Starting")}
	s.add(s.pending)
	return s
}

func (s *Session) add(it Item) {
	s.parts = append(s.parts, part{item: it})
}

// Apply folds one agent or judge event into the session.
func (s *Session) Apply(ev event.Event) {
	switch ev := ev.(type) {
	case event.AgentStarted:
		s.dropPending()
		s.attempt = ev.Attempt
		if ev.Agent != "" {
			s.Agent = ev.Agent
		}
		if ev.Model != "" {
			s.model = ev.Model
		}
		s.working = true
		s.thinking = &pendingItem{sty: s.sty, name: s.Agent, anim: newSpinner(s.sty, s.static, "thinking", "Thinking")}
		title := "Attempt " + strconv.Itoa(ev.Attempt) + " " + styles.Dot + " " + s.Agent
		if ev.Repair {
			title = "Attempt " + strconv.Itoa(ev.Attempt) + " " + styles.Dot + " repair"
		}
		s.add(&ruleItem{sty: s.sty, title: title})

	case event.AgentText:
		s.dropPending()
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			return
		}
		if last, ok := s.last().(*textItem); ok {
			last.text += "\n\n" + text
			return
		}
		s.add(&textItem{sty: s.sty, text: text})

	case event.AgentTool:
		s.dropPending()
		s.tool(ev.ID, ev.Name).call = ev

	case event.AgentToolResult:
		t := s.tool(ev.ID, ev.Name)
		t.result = &ev

	case event.AgentFinished:
		s.dropPending()
		s.working = false
		if ev.Model != "" {
			s.model = ev.Model
		}
		s.turns += ev.Turns
		s.cost += ev.CostUSD
		s.haltTools()
		s.add(&agentFooterItem{sty: s.sty, agent: s.Agent, model: s.model, fin: ev})

	case event.RunStarted:
		s.dropPending()
		s.working = false
		r := s.startJudge(ev.Path, ev.Network)
		r.Apply(ev)
		s.setJudge(ev.Path, ev.Scenario, event.Running)

	case event.ActorReady, event.StepStarted, event.StepFinished,
		event.AssertionStarted, event.AssertionFinished:
		s.judgeRun().Apply(ev)

	case event.Log:
		if r := s.Current(); r != nil && !r.Done() {
			r.Apply(ev)
			return
		}
		s.add(&logItem{sty: s.sty, log: ev})

	case event.RunFinished:
		r := s.judgeRun()
		r.Apply(ev)
		s.setJudge(r.Path, r.Info.Scenario, ev.Status)

	case event.JudgeFinished:
		s.add(&judgeSummaryItem{sty: s.sty, fin: ev})

	case event.LoopFinished:
		s.result = &ev
		s.cost = max(s.cost, ev.CostUSD)
		s.halt()
		s.add(&loopFooterItem{sty: s.sty, result: ev})
	}
}

// End is called when the agent func returns. A loop that never reported
// its result is closed as canceled or failed.
func (s *Session) End(err error) {
	if s.result != nil {
		s.done = true
		return
	}
	canceled := errors.Is(err, context.Canceled)
	if r := s.Current(); r != nil && !r.Done() {
		r.End(err)
	}
	res := event.LoopFinished{Status: event.Failed, Attempts: s.attempt, CostUSD: s.cost}
	if err != nil && !canceled {
		res.Error = err.Error()
	}
	s.result = &res
	s.canceled = canceled
	s.halt()
	s.add(&loopFooterItem{sty: s.sty, result: res, canceled: canceled})
}

func (s *Session) halt() {
	s.done = true
	s.working = false
	s.dropPending()
	s.haltTools()
}

func (s *Session) haltTools() {
	for _, t := range s.tools {
		t.halted = true
	}
}

func (s *Session) dropPending() {
	if s.pending == nil {
		return
	}
	for i, p := range s.parts {
		if p.item == Item(s.pending) {
			s.parts = append(s.parts[:i], s.parts[i+1:]...)
			break
		}
	}
	s.pending = nil
}

func (s *Session) last() Item {
	if len(s.parts) == 0 {
		return nil
	}
	return s.parts[len(s.parts)-1].item
}

func (s *Session) tool(id, name string) *toolItem {
	if t, ok := s.tools[id]; ok {
		return t
	}
	t := &toolItem{
		sty:  s.sty,
		call: event.AgentTool{ID: id, Name: name},
		anim: newSpinner(s.sty, s.static, "tool"+id, "Running"),
	}
	s.tools[id] = t
	s.add(t)
	return t
}

func (s *Session) startJudge(path, network string) *Run {
	if network == "" {
		network = s.Network
	}
	r := newRun(s.sty, path, network, s.static)
	s.runs = append(s.runs, r)
	s.add(&judgeRunItem{sty: s.sty, run: r})
	s.parts = append(s.parts, part{run: r})
	return r
}

// judgeRun is the run that judge events belong to. Events that arrive
// without a RunStarted still get a run to live in.
func (s *Session) judgeRun() *Run {
	if r := s.Current(); r != nil && !r.Done() {
		return r
	}
	s.dropPending()
	return s.startJudge("", "")
}

// setJudge records a judge status, matching by path, then scenario name,
// then file name. Judges the loop picked on its own are added to the list.
func (s *Session) setJudge(path, name string, status event.Status) {
	match := func(j Judge) bool {
		switch {
		case path != "" && j.Path == path:
			return true
		case name != "" && j.Name == name:
			return true
		}
		return name != "" && strings.TrimSuffix(filepath.Base(j.Path), filepath.Ext(j.Path)) == name
	}
	for i, j := range s.judges {
		if match(j) {
			s.judges[i].Status = status
			return
		}
	}
	if path == "" && name == "" {
		return
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	s.judges = append(s.judges, Judge{Name: name, Path: path, Status: status})
}

// Items is the session list in display order, judge runs included.
func (s *Session) Items() []Item {
	var out []Item
	for _, p := range s.parts {
		if p.run != nil {
			out = append(out, p.run.Items()...)
			continue
		}
		out = append(out, p.item)
	}
	if s.working && s.thinking != nil {
		if t, ok := s.last().(*toolItem); !ok || !t.spinning() {
			out = append(out, s.thinking)
		}
	}
	return out
}

// Spinning reports whether any item is animating.
func (s *Session) Spinning() bool {
	for _, it := range s.Items() {
		if sp, ok := it.(spinner); ok && sp.spinning() {
			return true
		}
	}
	return false
}

// Advance steps every running spinner by one frame.
func (s *Session) Advance() {
	for _, it := range s.Items() {
		if sp, ok := it.(spinner); ok && sp.spinning() {
			sp.advance()
		}
	}
}

// Current is the latest judge run, nil before the first one starts.
func (s *Session) Current() *Run {
	if len(s.runs) == 0 {
		return nil
	}
	return s.runs[len(s.runs)-1]
}

// Runs is every judge run so far, oldest first.
func (s *Session) Runs() []*Run { return s.runs }

func (s *Session) Judges() []Judge { return s.judges }
func (s *Session) Attempt() int    { return s.attempt }
func (s *Session) Model() string   { return s.model }
func (s *Session) Turns() int      { return s.turns }
func (s *Session) Cost() float64   { return s.cost }
func (s *Session) Done() bool      { return s.done }
func (s *Session) Canceled() bool  { return s.canceled }

// Result is the loop outcome, nil while it is still going.
func (s *Session) Result() *event.LoopFinished { return s.result }

// Stage is what the loop is doing, for headers: starting, working,
// judging or done.
func (s *Session) Stage() string {
	switch {
	case s.done:
		return "done"
	case s.working:
		return "working"
	case s.Current() != nil && !s.Current().Done():
		return "judging"
	case s.attempt == 0:
		return "starting"
	}
	return "waiting"
}
