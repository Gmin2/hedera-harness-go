package ops

import (
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

type topicCreate struct {
	Common    `yaml:",inline"`
	As        string `yaml:"as"`
	TopicMemo string `yaml:"topic_memo"`
	SubmitKey string `yaml:"submit_key"`
	AdminKey  string `yaml:"admin_key"`
}

func (o *topicCreate) Target() string { return o.As }
func (o *topicCreate) Params() []event.Param {
	var p []event.Param
	if o.SubmitKey != "" {
		p = append(p, param("submit_key", o.SubmitKey))
	}
	if o.TopicMemo != "" {
		p = append(p, param("memo", o.TopicMemo))
	}
	return p
}

func (o *topicCreate) Build(env *Env) (*Tx, error) {
	if o.As == "" {
		return nil, fmt.Errorf("topic.create needs as: <name>")
	}
	tx := hiero.NewTopicCreateTransaction()
	if o.TopicMemo != "" {
		tx.SetTopicMemo(o.TopicMemo)
	}
	if o.SubmitKey != "" {
		k, err := env.PublicKey(o.SubmitKey)
		if err != nil {
			return nil, err
		}
		tx.SetSubmitKey(k)
	}
	if o.AdminKey != "" {
		k, err := env.PublicKey(o.AdminKey)
		if err != nil {
			return nil, err
		}
		tx.SetAdminKey(k)
	}
	return &Tx{
		Tx:           tx,
		Signers:      signers(&o.Common, o.AdminKey),
		Declares:     o.As,
		DeclaresKind: "topic",
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			if r.TopicID == nil {
				return nil, fmt.Errorf("receipt has no topic id")
			}
			return []event.Param{param(o.As, r.TopicID)}, env.Bind(o.As, "topic", r.TopicID.String())
		},
	}, nil
}

type topicSubmit struct {
	Common  `yaml:",inline"`
	Topic   string `yaml:"topic"`
	Message string `yaml:"message"`
	Signer  string `yaml:"submit_key"` // actor holding the submit key
}

func (o *topicSubmit) Target() string { return o.Topic }
func (o *topicSubmit) Params() []event.Param {
	msg := o.Message
	if len(msg) > 40 {
		msg = msg[:37] + "..."
	}
	return []event.Param{param("message", msg)}
}

func (o *topicSubmit) Build(env *Env) (*Tx, error) {
	id, err := env.Topic(o.Topic)
	if err != nil {
		return nil, err
	}
	if o.Message == "" {
		return nil, fmt.Errorf("topic.submit needs a message")
	}
	tx := hiero.NewTopicMessageSubmitTransaction().SetTopicID(id).SetMessage(o.Message)
	return &Tx{
		Tx:      tx,
		Signers: signers(&o.Common, o.Signer),
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			return []event.Param{param("sequence", r.TopicSequenceNumber)}, nil
		},
	}, nil
}

func init() {
	register("topic.create", func() Op { return &topicCreate{} })
	register("topic.submit", func() Op { return &topicSubmit{} })
}
