// Package ops maps scenario steps to sdk transactions.
package ops

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

// Common fields every step accepts.
type Common struct {
	// Expect is the receipt or precheck status the step should end with.
	// Defaults to SUCCESS, so negative tests read as `expect: INVALID_SIGNATURE`.
	Expect string `yaml:"expect"`
	// Payer pays the fee instead of the operator and signs.
	Payer string `yaml:"payer"`
	// Signers replaces the signers hh would pick, to test missing signatures.
	Signers *[]string `yaml:"signers"`
	Memo    string    `yaml:"memo"`
}

func (c *Common) common() *Common { return c }

type Op interface {
	common() *Common
	// Target is the main thing the step acts on, shown next to the op name.
	Target() string
	Params() []event.Param
	Build(env *Env) (*Tx, error)
}

// Tx is a built but not yet frozen transaction.
type Tx struct {
	Tx      hiero.TransactionInterface
	Signers []string
	// Declares is the name the step binds on success, if any, and its kind.
	Declares     string
	DeclaresKind string
	// Bind reads new entity ids from the receipt.
	Bind func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error)
}

var registry = map[string]func() Op{}

func register(name string, f func() Op) { registry[name] = f }

// Names lists every supported step op.
func Names() []string {
	var out []string
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Decode(s scenario.Step) (Op, error) {
	f, ok := registry[s.Op]
	if !ok {
		return nil, fmt.Errorf("line %d: unknown step %q (known: %s)", s.Line, s.Op, strings.Join(Names(), ", "))
	}
	op := f()
	if err := s.Decode(op); err != nil {
		return nil, err
	}
	return op, nil
}

// Settings returns the common fields of a decoded op.
func Settings(op Op) Common { return *op.common() }

func param(k string, v any) event.Param {
	return event.Param{Key: k, Value: fmt.Sprint(v)}
}

func hbar(amount float64) hiero.Hbar {
	return hiero.HbarFromTinybar(int64(amount*1e8 + sign(amount)*0.5))
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

func fmtHbar(amount float64) string {
	return strconv.FormatFloat(amount, 'f', -1, 64) + " ℏ"
}

// signers picks override signers when the step sets them.
func signers(c *Common, defaults ...string) []string {
	if c.Signers != nil {
		return *c.Signers
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range defaults {
		// operator stays in the list: when another actor pays, the operator
		// may still hold a token key. the sdk skips keys that already signed.
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func orOperator(ref string) string {
	if ref == "" {
		return "operator"
	}
	return ref
}

// Fields lists the yaml fields a step op accepts.
func Fields(name string) []string {
	f, ok := registry[name]
	if !ok {
		return nil
	}
	return scenario.Fields(f())
}
