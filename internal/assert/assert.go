// Package assert checks chain state on the mirror node.
//
// Every check reads real mirror data and polls until it matches or times
// out, so there are no fixed sleeps and a failure always carries the last
// observed value and the url it came from.
package assert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

type Check interface {
	Title() string
	Expected() string
	// Resolve checks names against the env. It runs while planning too.
	Resolve(env *ops.Env) error
	// Observe reads state once and says whether it matches.
	Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error)
}

type Observation struct {
	Actual string
	OK     bool
	Source string
}

var registry = map[string]func() Check{}

func register(name string, f func() Check) { registry[name] = f }

func Names() []string {
	var out []string
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Decode(s scenario.Step) (Check, error) {
	f, ok := registry[s.Op]
	if !ok {
		return nil, fmt.Errorf("line %d: unknown assertion %q (known: %s)", s.Line, s.Op, strings.Join(Names(), ", "))
	}
	c := f()
	if err := s.Decode(c); err != nil {
		return nil, err
	}
	return c, nil
}

type Poll struct {
	Timeout  time.Duration
	Interval time.Duration
}

// Eval polls one check until it matches or the timeout passes.
func Eval(ctx context.Context, env *ops.Env, m *mirror.Client, c Check, p Poll) event.AssertionFinished {
	start := time.Now()
	res := event.AssertionFinished{Title: c.Title(), Expected: c.Expected()}
	if p.Interval == 0 {
		p.Interval = 500 * time.Millisecond
	}
	deadline := start.Add(p.Timeout)

	for {
		res.Attempts++
		obs, err := c.Observe(ctx, env, m)
		res.Source = obs.Source
		switch {
		case err == nil:
			res.Actual, res.Error = obs.Actual, ""
			if obs.OK {
				res.Status = event.Passed
				res.Elapsed = time.Since(start)
				return res
			}
		case errors.Is(err, mirror.ErrNotFound):
			res.Actual, res.Error = "not found", ""
		default:
			res.Error = err.Error()
		}
		if time.Now().Add(p.Interval).After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			res.Error = ctx.Err().Error()
			res.Status = event.Failed
			res.Elapsed = time.Since(start)
			return res
		case <-time.After(p.Interval):
		}
	}
	res.Status = event.Failed
	res.Elapsed = time.Since(start)
	return res
}

// Num is a numeric comparison. Values are in the unit of the check
// (hbar for account.hbar, smallest units for tokens).
type Num struct {
	Equals *float64 `yaml:"equals"`
	Not    *float64 `yaml:"not"`
	Gt     *float64 `yaml:"gt"`
	Gte    *float64 `yaml:"gte"`
	Lt     *float64 `yaml:"lt"`
	Lte    *float64 `yaml:"lte"`
}

func (n Num) empty() bool {
	return n.Equals == nil && n.Not == nil && n.Gt == nil && n.Gte == nil && n.Lt == nil && n.Lte == nil
}

// match compares an integer actual against expectations scaled to integers,
// eg scale 1e8 turns hbar into tinybars so nothing is compared in floats.
func (n Num) match(actual int64, scale float64) bool {
	to := func(f *float64) int64 { return int64(math.Round(*f * scale)) }
	ok := true
	if n.Equals != nil {
		ok = ok && actual == to(n.Equals)
	}
	if n.Not != nil {
		ok = ok && actual != to(n.Not)
	}
	if n.Gt != nil {
		ok = ok && actual > to(n.Gt)
	}
	if n.Gte != nil {
		ok = ok && actual >= to(n.Gte)
	}
	if n.Lt != nil {
		ok = ok && actual < to(n.Lt)
	}
	if n.Lte != nil {
		ok = ok && actual <= to(n.Lte)
	}
	return ok
}

func (n Num) String(unit string) string {
	var parts []string
	add := func(op string, f *float64) {
		if f != nil {
			parts = append(parts, op+" "+strconv.FormatFloat(*f, 'f', -1, 64)+unit)
		}
	}
	add("=", n.Equals)
	add("≠", n.Not)
	add(">", n.Gt)
	add("≥", n.Gte)
	add("<", n.Lt)
	add("≤", n.Lte)
	return strings.Join(parts, " and ")
}

func requireNum(n Num, kind string) error {
	if n.empty() {
		return fmt.Errorf("%s needs one of equals, not, gt, gte, lt, lte", kind)
	}
	return nil
}

func lookup(env *ops.Env, ref, what string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("missing %s", what)
	}
	id, ok := env.Lookup(ref)
	if !ok {
		return "", fmt.Errorf("unknown %s %q", what, ref)
	}
	return id, nil
}

func tinybarsToHbar(t int64) string {
	return strconv.FormatFloat(float64(t)/1e8, 'f', -1, 64) + " ℏ"
}

// Fields lists the yaml fields an assertion accepts.
func Fields(name string) []string {
	f, ok := registry[name]
	if !ok {
		return nil
	}
	return scenario.Fields(f())
}
