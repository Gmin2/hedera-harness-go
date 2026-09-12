package ops

import (
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"gopkg.in/yaml.v3"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

// schedulable lists the ops that may be wrapped in schedule.create.
var schedulable = map[string]bool{
	"hbar.transfer":   true,
	"token.transfer":  true,
	"token.mint":      true,
	"token.associate": true,
	"topic.submit":    true,
}

type scheduleCreate struct {
	Common        `yaml:",inline"`
	As            string    `yaml:"as"`
	SchedulePayer string    `yaml:"schedule_payer"`
	AdminKey      string    `yaml:"admin_key"`
	ScheduleMemo  string    `yaml:"schedule_memo"`
	WaitForExpiry bool      `yaml:"wait_for_expiry"`
	Inner         yaml.Node `yaml:"tx"`

	inner Op
	op    string
}

func (o *scheduleCreate) Target() string { return o.As }
func (o *scheduleCreate) Params() []event.Param {
	// params are shown before Build runs, a bad inner tx is reported by Build
	_ = o.decodeInner()
	p := []event.Param{param("tx", o.op)}
	if o.inner != nil {
		if t := o.inner.Target(); t != "" {
			p = append(p, param("on", t))
		}
	}
	return p
}

func (o *scheduleCreate) decodeInner() error {
	if o.inner != nil {
		return nil
	}
	if o.Inner.Kind != yaml.MappingNode || len(o.Inner.Content) != 2 {
		return fmt.Errorf("schedule.create needs tx: { <op>: {...} }")
	}
	step := scenario.Step{Op: o.Inner.Content[0].Value, Line: o.Inner.Content[0].Line, Params: o.Inner.Content[1]}
	if !schedulable[step.Op] {
		return fmt.Errorf("line %d: %s cannot be scheduled by hh yet", step.Line, step.Op)
	}
	inner, err := Decode(step)
	if err != nil {
		return err
	}
	o.inner, o.op = inner, step.Op
	return nil
}

func (o *scheduleCreate) Build(env *Env) (*Tx, error) {
	if o.As == "" {
		return nil, fmt.Errorf("schedule.create needs as: <name>")
	}
	if err := o.decodeInner(); err != nil {
		return nil, err
	}
	innerTx, err := o.inner.Build(env)
	if err != nil {
		return nil, fmt.Errorf("scheduled %s: %w", o.op, err)
	}
	tx := hiero.NewScheduleCreateTransaction()
	if _, err := tx.SetScheduledTransaction(innerTx.Tx); err != nil {
		return nil, err
	}
	if o.SchedulePayer != "" {
		id, err := env.Account(o.SchedulePayer)
		if err != nil {
			return nil, err
		}
		tx.SetPayerAccountID(id)
	}
	if o.AdminKey != "" {
		k, err := env.PublicKey(o.AdminKey)
		if err != nil {
			return nil, err
		}
		tx.SetAdminKey(k)
	}
	if o.ScheduleMemo != "" {
		tx.SetScheduleMemo(o.ScheduleMemo)
	}
	tx.SetWaitForExpiry(o.WaitForExpiry)

	// nobody signs the inner tx by default, collecting signatures is the
	// point of a schedule. use signers: [...] to sign at creation.
	var sign []string
	if o.Signers != nil {
		sign = *o.Signers
	}
	if o.AdminKey != "" {
		sign = append(sign, o.AdminKey)
	}
	return &Tx{
		Tx:           tx,
		Signers:      sign,
		Declares:     o.As,
		DeclaresKind: "schedule",
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			if r.ScheduleID == nil {
				return nil, fmt.Errorf("receipt has no schedule id")
			}
			p := []event.Param{param(o.As, r.ScheduleID)}
			if r.ScheduledTransactionID != nil {
				p = append(p, param("scheduled_tx", r.ScheduledTransactionID))
			}
			return p, env.Bind(o.As, "schedule", r.ScheduleID.String())
		},
	}, nil
}

type scheduleSign struct {
	Common   `yaml:",inline"`
	Schedule string `yaml:"schedule"`
	Signer   string `yaml:"signer"`
}

func (o *scheduleSign) Target() string        { return o.Schedule }
func (o *scheduleSign) Params() []event.Param { return []event.Param{param("signer", o.Signer)} }

func (o *scheduleSign) Build(env *Env) (*Tx, error) {
	id, err := env.Schedule(o.Schedule)
	if err != nil {
		return nil, err
	}
	if o.Signer == "" && o.Signers == nil {
		return nil, fmt.Errorf("schedule.sign needs signer: <actor>")
	}
	tx := hiero.NewScheduleSignTransaction().SetScheduleID(id)
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.Signer)}, nil
}

func init() {
	register("schedule.create", func() Op { return &scheduleCreate{} })
	register("schedule.sign", func() Op { return &scheduleSign{} })
}
