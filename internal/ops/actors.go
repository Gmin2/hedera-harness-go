package ops

import (
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/event"
	"github.com/Gmin2/hedera-harness-go/internal/keys"
	"github.com/Gmin2/hedera-harness-go/internal/scenario"
)

const defaultActorHbar = 10

// PrepareActor makes keys for a scenario actor and, when it needs a new
// account, a create transaction. Existing accounts (id + private_key) need none.
func PrepareActor(env *Env, spec scenario.Actor) (*Actor, *Tx, error) {
	a := &Actor{Name: spec.Name}

	if spec.ID != "" {
		id, err := hiero.AccountIDFromString(spec.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("actor %s: %w", spec.Name, err)
		}
		if spec.PrivateKey == "" {
			return nil, nil, fmt.Errorf("actor %s: an existing account needs private_key", spec.Name)
		}
		k, err := keys.Parse(spec.PrivateKey, spec.Key)
		if err != nil {
			return nil, nil, fmt.Errorf("actor %s: %w", spec.Name, err)
		}
		a.ID, a.Key, a.SignKeys, a.KeyType = id, k.PublicKey(), []hiero.PrivateKey{k}, keys.Kind(k)
		return a, nil, nil
	}

	tx := hiero.NewAccountCreateTransaction()
	var signWith []string

	switch {
	case spec.Threshold > 0 || len(spec.Keys) > 0:
		if len(spec.Keys) == 0 {
			return nil, nil, fmt.Errorf("actor %s: threshold needs keys: [actor, ...]", spec.Name)
		}
		threshold := spec.Threshold
		if threshold == 0 {
			threshold = uint(len(spec.Keys))
		}
		if int(threshold) > len(spec.Keys) {
			return nil, nil, fmt.Errorf("actor %s: threshold %d is more than %d keys", spec.Name, threshold, len(spec.Keys))
		}
		list := hiero.KeyListWithThreshold(threshold)
		for _, m := range spec.Keys {
			member, err := env.Actor(m)
			if err != nil {
				return nil, nil, fmt.Errorf("actor %s: key member: %w (declare it above)", spec.Name, err)
			}
			list.Add(member.Key)
			a.SignKeys = append(a.SignKeys, member.SignKeys...)
		}
		a.Key = list
		a.KeyType = fmt.Sprintf("%d of %d", threshold, len(spec.Keys))
		tx.SetKeyWithoutAlias(list)
	default:
		kind := spec.Key
		if kind == "" {
			kind = keys.ECDSA
		}
		k, err := keys.Generate(kind)
		if err != nil {
			return nil, nil, fmt.Errorf("actor %s: %w", spec.Name, err)
		}
		a.Key, a.SignKeys, a.KeyType = k.PublicKey(), []hiero.PrivateKey{k}, keys.Kind(k)
		if a.KeyType == keys.ECDSA {
			// the alias key must sign the create
			tx.SetECDSAKeyWithAlias(k.PublicKey())
			a.EvmAlias = k.PublicKey().ToEvmAddress()
			signWith = []string{spec.Name}
		} else {
			tx.SetKeyWithoutAlias(k.PublicKey())
		}
	}

	balance := float64(defaultActorHbar)
	if spec.Hbar != nil {
		balance = *spec.Hbar
	}
	tx.SetInitialBalance(hbar(balance))
	if spec.MaxAutoAssociations != nil {
		tx.SetMaxAutomaticTokenAssociations(*spec.MaxAutoAssociations)
	}
	tx.SetReceiverSignatureRequired(spec.ReceiverSigRequired)
	if spec.ReceiverSigRequired {
		signWith = append(signWith, spec.Name)
	}
	if spec.Memo != "" {
		tx.SetAccountMemo(spec.Memo)
	}

	return a, &Tx{
		Tx:      tx,
		Signers: signWith,
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			if r.AccountID == nil {
				return nil, fmt.Errorf("receipt for %s has no account id", spec.Name)
			}
			a.ID = *r.AccountID
			return []event.Param{param(spec.Name, a.ID)}, nil
		},
	}, nil
}

type hbarTransfer struct {
	Common `yaml:",inline"`
	From   string  `yaml:"from"`
	To     string  `yaml:"to"`
	Amount float64 `yaml:"amount"` // hbar
}

func (o *hbarTransfer) Target() string { return o.From + " → " + o.To }
func (o *hbarTransfer) Params() []event.Param {
	return []event.Param{param("amount", fmtHbar(o.Amount))}
}

func (o *hbarTransfer) Build(env *Env) (*Tx, error) {
	if o.Amount <= 0 {
		return nil, fmt.Errorf("amount must be more than 0")
	}
	from, err := env.Account(orOperator(o.From))
	if err != nil {
		return nil, err
	}
	to, err := env.Account(o.To)
	if err != nil {
		return nil, err
	}
	tx := hiero.NewTransferTransaction().
		AddHbarTransfer(from, hbar(-o.Amount)).
		AddHbarTransfer(to, hbar(o.Amount))
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.From)}, nil
}

type accountUpdate struct {
	Common              `yaml:",inline"`
	Account             string `yaml:"account"`
	MaxAutoAssociations *int32 `yaml:"max_auto_associations"`
	Memo                string `yaml:"account_memo"`
}

func (o *accountUpdate) Target() string { return o.Account }
func (o *accountUpdate) Params() []event.Param {
	var p []event.Param
	if o.MaxAutoAssociations != nil {
		p = append(p, param("max_auto_associations", *o.MaxAutoAssociations))
	}
	return p
}

func (o *accountUpdate) Build(env *Env) (*Tx, error) {
	id, err := env.Account(o.Account)
	if err != nil {
		return nil, err
	}
	tx := hiero.NewAccountUpdateTransaction().SetAccountID(id)
	if o.MaxAutoAssociations != nil {
		tx.SetMaxAutomaticTokenAssociations(*o.MaxAutoAssociations)
	}
	if o.Memo != "" {
		tx.SetAccountMemo(o.Memo)
	}
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.Account)}, nil
}

func init() {
	register("hbar.transfer", func() Op { return &hbarTransfer{} })
	register("account.update", func() Op { return &accountUpdate{} })
}
