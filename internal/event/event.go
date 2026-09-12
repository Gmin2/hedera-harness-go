// Package event defines what a scenario run reports while it is running.
// The runner produces these, the plain printer and the tui consume them.
package event

import "time"

type Event interface{ event() }

type Status string

const (
	Pending Status = "pending"
	Running Status = "running"
	Passed  Status = "passed"
	Failed  Status = "failed"
	Skipped Status = "skipped"
)

type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type RunStarted struct {
	RunID      string    `json:"run_id"`
	Scenario   string    `json:"scenario"`
	Path       string    `json:"path,omitempty"`
	Network    string    `json:"network"`
	Operator   string    `json:"operator"`
	Actors     int       `json:"actors"`
	Steps      int       `json:"steps"`
	Assertions int       `json:"assertions"`
	At         time.Time `json:"at"`
}

type ActorReady struct {
	Name    string `json:"name"`
	Account string `json:"account"`
	KeyType string `json:"key_type"`
	Hbar    string `json:"hbar,omitempty"`
	TxID    string `json:"tx_id,omitempty"`
	Link    string `json:"link,omitempty"`
}

type StepStarted struct {
	Index  int     `json:"index"`
	Op     string  `json:"op"`
	Target string  `json:"target"`
	Params []Param `json:"params,omitempty"`
}

type StepFinished struct {
	Index    int           `json:"index"`
	Op       string        `json:"op"`
	Status   Status        `json:"status"`
	Receipt  string        `json:"receipt,omitempty"`  // SUCCESS, TOKEN_NOT_ASSOCIATED_TO_ACCOUNT, ...
	Expected string        `json:"expected,omitempty"` // status the scenario expected, SUCCESS by default
	TxID     string        `json:"tx_id,omitempty"`
	Link     string        `json:"link,omitempty"`
	Entities []Param       `json:"entities,omitempty"` // bound names, eg gold = 0.0.1004
	Error    string        `json:"error,omitempty"`
	Elapsed  time.Duration `json:"elapsed_ns"`
}

type AssertionStarted struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type AssertionFinished struct {
	Index    int           `json:"index"`
	Kind     string        `json:"kind"`
	Title    string        `json:"title"`
	Status   Status        `json:"status"`
	Expected string        `json:"expected,omitempty"`
	Actual   string        `json:"actual"`
	Source   string        `json:"source,omitempty"` // mirror url the value came from
	Attempts int           `json:"attempts"`
	Error    string        `json:"error,omitempty"`
	Elapsed  time.Duration `json:"elapsed_ns"`
}

type Log struct {
	Level string `json:"level"` // info, warn, error
	Msg   string `json:"msg"`
}

type RunFinished struct {
	RunID      string        `json:"run_id"`
	Status     Status        `json:"status"`
	StepsOK    int           `json:"steps_ok"`
	StepsFail  int           `json:"steps_fail"`
	AssertOK   int           `json:"assert_ok"`
	AssertFail int           `json:"assert_fail"`
	Elapsed    time.Duration `json:"elapsed_ns"`
	Error      string        `json:"error,omitempty"`
}

func (RunStarted) event()        {}
func (ActorReady) event()        {}
func (StepStarted) event()       {}
func (StepFinished) event()      {}
func (AssertionStarted) event()  {}
func (AssertionFinished) event() {}
func (Log) event()               {}
func (RunFinished) event()       {}

// Sink receives events. It must not block for long.
type Sink func(Event)
