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

// Check is a shell command that judges the agent's work, with the status of
// its latest run.
type Check struct {
	Name    string
	Command string
	Status  event.Status
}

// Session is an agent conversation as seen by the ui. Every prompt sent with
// Send is a turn: the prompt, each attempt of the coding agent, the tools it
// called and the judge runs that checked the result. Turns stack in one list.
type Session struct {
	sty    *styles.Styles
	static bool

	Prompt  string // the latest prompt
	Agent   string
	Network string

	parts     []part
	runs      []*Run
	model     string
	sessionID string
	cost      float64
	turn      int

	// state of the current turn, reset by Send
	pending     *pendingItem
	thinking    *pendingItem
	live        *textItem
	tools       map[string]*toolItem
	judges      []Judge
	checks      []Check
	checkItems  []*checkItem // checks of the current attempt
	runStart    int
	attempt     int
	maxAttempts int
	turnCost    float64
	footers     int
	judged      bool
	judging     bool // between the first check or judge run and JudgeFinished
	working     bool
	result      *event.LoopFinished
	canceled    bool
	done        bool
}

// part is either a plain item or a judge run whose items are spliced in.
type part struct {
	item Item
	run  *Run
}

// NewSession starts an empty conversation. agent is the display name of the
// coding agent. Nothing runs until the first Send.
func NewSession(sty *styles.Styles, agent string, static bool) *Session {
	return &Session{sty: sty, static: static, Agent: agent, done: true}
}

// Send starts a new turn for prompt under the previous ones. judges and
// checks are what the loop verifies this turn with.
func (s *Session) Send(prompt, network string, judges []Judge, checks []Check) {
	s.halt()
	s.Prompt = prompt
	s.Network = network
	s.turn++
	s.tools = make(map[string]*toolItem)
	s.judges = append([]Judge(nil), judges...)
	s.checks = append([]Check(nil), checks...)
	s.checkItems = nil
	s.runStart = len(s.runs)
	s.attempt, s.maxAttempts = 0, 0
	s.turnCost = 0
	s.footers = 0
	s.judged = false
	s.result = nil
	s.canceled = false
	s.done = false

	s.add(&promptItem{sty: s.sty, text: prompt})
	s.pending = &pendingItem{sty: s.sty, name: s.Agent, anim: newSpinner(s.sty, s.static, "agent"+strconv.Itoa(s.turn), "Starting")}
	s.add(s.pending)
}

func (s *Session) add(it Item) {
	s.parts = append(s.parts, part{item: it})
}

// Apply folds one agent or judge event into the current turn.
func (s *Session) Apply(ev event.Event) {
	switch ev := ev.(type) {
	case event.AgentStarted:
		s.dropPending()
		s.closeText()
		s.attempt = ev.Attempt
		if ev.MaxAttempts > 0 {
			s.maxAttempts = ev.MaxAttempts
		}
		if ev.Turn > 0 {
			s.turn = ev.Turn
		}
		if ev.Agent != "" {
			s.Agent = ev.Agent
		}
		if ev.Model != "" {
			s.model = ev.Model
		}
		s.working = true
		s.judging = false
		s.checkItems = nil
		s.thinking = &pendingItem{sty: s.sty, name: s.Agent, anim: newSpinner(s.sty, s.static, "thinking", "Thinking")}
		// Repairs get a rule. The first attempt only gets one when it opens
		// a judged conversation, later turns read like a chat.
		switch {
		case ev.Repair || ev.Attempt > 1:
			s.add(&ruleItem{sty: s.sty, title: "Attempt " + strconv.Itoa(ev.Attempt) + " " + styles.Dot + " repair"})
		case s.first() && (len(s.judges) > 0 || len(s.checks) > 0):
			s.add(&ruleItem{sty: s.sty, title: "Attempt " + strconv.Itoa(ev.Attempt) + " " + styles.Dot + " " + s.Agent})
		}

	case event.AgentTextDelta:
		s.dropPending()
		if ev.Text == "" {
			return
		}
		if s.live == nil || s.live.attempt != ev.Attempt || s.last() != Item(s.live) {
			s.live = &textItem{sty: s.sty, attempt: ev.Attempt, live: true}
			s.add(s.live)
		}
		s.live.text += ev.Text

	case event.AgentText:
		s.dropPending()
		text := strings.TrimSpace(ev.Text)
		if live := s.live; live != nil && live.attempt == ev.Attempt {
			s.live = nil
			live.live = false
			live.text = text
			if text == "" {
				s.remove(live)
			}
			return
		}
		if text == "" {
			return
		}
		if last, ok := s.last().(*textItem); ok && !last.live {
			last.text += "\n\n" + text
			return
		}
		s.add(&textItem{sty: s.sty, attempt: ev.Attempt, text: text})

	case event.AgentTool:
		s.dropPending()
		s.closeText()
		s.tool(ev.ID, ev.Name).call = ev

	case event.AgentToolResult:
		s.closeText()
		t := s.tool(ev.ID, ev.Name)
		t.result = &ev

	case event.AgentFinished:
		s.dropPending()
		s.closeText()
		s.working = false
		if ev.Model != "" {
			s.model = ev.Model
		}
		if ev.SessionID != "" {
			s.sessionID = ev.SessionID
		}
		s.turnCost += ev.CostUSD
		s.cost += ev.CostUSD
		s.haltTools()
		s.footers++
		s.add(&agentFooterItem{sty: s.sty, agent: s.Agent, model: s.model, fin: ev})

	case event.CheckStarted:
		s.dropPending()
		s.closeText()
		s.working = false
		s.judged, s.judging = true, true
		s.checkItem(ev.Name, ev.Command)
		s.setCheck(ev.Name, ev.Command, event.Running)

	case event.CheckFinished:
		s.dropPending()
		s.closeText()
		s.working = false
		s.judged, s.judging = true, true
		c := s.checkItem(ev.Name, ev.Command)
		c.fin = &ev
		s.setCheck(ev.Name, ev.Command, ev.Status)

	case event.RunStarted:
		s.dropPending()
		s.closeText()
		s.working = false
		s.judged, s.judging = true, true
		r := s.startJudge(ev.Path, ev.Network)
		r.Apply(ev)
		s.setJudge(ev.Path, ev.Scenario, event.Running)

	case event.ActorReady, event.StepStarted, event.StepFinished,
		event.AssertionStarted, event.AssertionFinished:
		s.judged = true
		s.judgeRun().Apply(ev)

	case event.Log:
		if r := s.Current(); r != nil && !r.Done() {
			r.Apply(ev)
			return
		}
		s.closeText()
		s.add(&logItem{sty: s.sty, log: ev})

	case event.RunFinished:
		s.judged = true
		r := s.judgeRun()
		r.Apply(ev)
		s.setJudge(r.Path, r.Info.Scenario, ev.Status)

	case event.JudgeFinished:
		s.judged = true
		s.judging = false
		sum := &judgeSummaryItem{sty: s.sty, fin: ev}
		for _, c := range s.checkItems {
			sum.checks++
			if c.fin != nil && c.fin.Status != event.Passed {
				sum.checksFailed++
			}
		}
		s.add(sum)

	case event.LoopFinished:
		s.result = &ev
		if ev.SessionID != "" {
			s.sessionID = ev.SessionID
		}
		if ev.CostUSD > s.turnCost {
			s.cost += ev.CostUSD - s.turnCost
			s.turnCost = ev.CostUSD
		}
		s.halt()
		// A plain chat turn already closed with its agent footer.
		if s.judged || ev.Status != event.Passed || s.footers == 0 {
			s.add(&loopFooterItem{sty: s.sty, result: ev})
		}
	}
}

// End is called when the agent func returns. A turn that never reported its
// result is closed as canceled or failed. The session id is kept either way
// so the conversation can go on.
func (s *Session) End(err error) {
	if s.result != nil {
		s.done = true
		return
	}
	canceled := errors.Is(err, context.Canceled)
	if r := s.Current(); r != nil && !r.Done() {
		r.End(err)
	}
	res := event.LoopFinished{Status: event.Failed, SessionID: s.sessionID, Attempts: s.attempt, CostUSD: s.turnCost}
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
	s.judging = false
	s.dropPending()
	s.closeText()
	s.haltTools()
	for _, c := range s.checkItems {
		c.halted = true
	}
}

func (s *Session) haltTools() {
	for _, t := range s.tools {
		t.halted = true
	}
}

// closeText stops the live text item from taking more deltas.
func (s *Session) closeText() {
	if s.live != nil {
		s.live.live = false
		s.live = nil
	}
}

func (s *Session) dropPending() {
	if s.pending == nil {
		return
	}
	s.remove(s.pending)
	s.pending = nil
}

func (s *Session) remove(it Item) {
	for i, p := range s.parts {
		if p.item == it {
			s.parts = append(s.parts[:i], s.parts[i+1:]...)
			return
		}
	}
}

func (s *Session) last() Item {
	if len(s.parts) == 0 {
		return nil
	}
	return s.parts[len(s.parts)-1].item
}

// first reports whether the current turn is the one that opened the list.
func (s *Session) first() bool {
	if len(s.parts) == 0 {
		return true
	}
	for _, p := range s.parts[1:] {
		if _, ok := p.item.(*promptItem); ok {
			return false
		}
	}
	return true
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

// checkItem finds the item for a check of the current attempt, adding one
// when the check has not been seen yet.
func (s *Session) checkItem(name, command string) *checkItem {
	if name == "" {
		name = command
	}
	for _, c := range s.checkItems {
		if c.name == name && c.fin == nil {
			return c
		}
	}
	c := &checkItem{
		sty:     s.sty,
		name:    name,
		command: command,
		anim:    newSpinner(s.sty, s.static, "check"+strconv.Itoa(s.attempt)+name, "Running"),
	}
	s.checkItems = append(s.checkItems, c)
	s.add(c)
	return c
}

// setCheck records a check status by name, then command. Checks the loop
// ran without being asked for are added to the list.
func (s *Session) setCheck(name, command string, status event.Status) {
	if name == "" {
		name = command
	}
	for i, c := range s.checks {
		if c.Name == name || command != "" && c.Command == command {
			s.checks[i].Status = status
			return
		}
	}
	if name == "" {
		return
	}
	s.checks = append(s.checks, Check{Name: name, Command: command, Status: status})
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
	s.closeText()
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

// Items is the conversation in display order, judge runs included.
func (s *Session) Items() []Item {
	var out []Item
	for _, p := range s.parts {
		if p.run != nil {
			out = append(out, p.run.Items()...)
			continue
		}
		out = append(out, p.item)
	}
	if s.working && s.thinking != nil && s.live == nil {
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

// Current is the latest judge run of this turn, nil before one starts.
func (s *Session) Current() *Run {
	if len(s.runs) == s.runStart {
		return nil
	}
	return s.runs[len(s.runs)-1]
}

// Runs is every judge run in the conversation, oldest first.
func (s *Session) Runs() []*Run { return s.runs }

func (s *Session) Judges() []Judge { return s.judges }
func (s *Session) Checks() []Check { return s.checks }
func (s *Session) Attempt() int    { return s.attempt }
func (s *Session) Model() string   { return s.model }
func (s *Session) Done() bool      { return s.done }
func (s *Session) Canceled() bool  { return s.canceled }

// MaxAttempts is how many attempts the loop allows per turn, 0 if unknown.
func (s *Session) MaxAttempts() int { return s.maxAttempts }

// Turn is the number of prompts sent in this conversation.
func (s *Session) Turn() int { return s.turn }

// Cost is what the whole conversation has spent so far.
func (s *Session) Cost() float64 { return s.cost }

// Judged reports whether judges ran in the current turn.
func (s *Session) Judged() bool { return s.judged }

// SessionID is the agent session to continue with the next prompt, empty
// until the agent reports one.
func (s *Session) SessionID() string { return s.sessionID }

// Result is the outcome of the current turn, nil while it is still going.
func (s *Session) Result() *event.LoopFinished { return s.result }

// Stage is what the current turn is doing, for headers: starting, working,
// judging or done.
func (s *Session) Stage() string {
	switch {
	case s.done:
		return "done"
	case s.working:
		return "working"
	case s.judging, s.RunningCheck() != "":
		return "judging"
	case s.Current() != nil && !s.Current().Done():
		return "judging"
	case s.attempt == 0:
		return "starting"
	}
	return "waiting"
}

// RunningCheck is the name of the check running right now, empty when none is.
func (s *Session) RunningCheck() string {
	for _, c := range s.checkItems {
		if c.spinning() {
			return c.name
		}
	}
	return ""
}
