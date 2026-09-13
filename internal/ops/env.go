package ops

import (
	"fmt"
	"strconv"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/network"
)

// Actor is a named account the scenario can pay from, sign with and refer to.
type Actor struct {
	Name     string
	ID       hiero.AccountID
	Key      hiero.Key          // what is on chain: a public key or a key list
	SignKeys []hiero.PrivateKey // keys hh signs with for this actor
	KeyType  string             // ecdsa, ed25519, 2 of 3
	EvmAlias string
}

// Env holds everything a step can refer to by name.
// When Target is nil the env is planning: names resolve to placeholders
// so a scenario can be checked without touching a network.
type Env struct {
	Target   *network.Target
	actors   map[string]*Actor
	entities map[string]entity
	nextFake int64
	// scheduledTx maps a schedule name to the id its inner transaction runs as
	scheduledTx map[string]string
}

type entity struct {
	kind string // token, topic, schedule
	id   string
}

func NewEnv(t *network.Target) *Env {
	e := &Env{Target: t, actors: map[string]*Actor{}, entities: map[string]entity{}, nextFake: 9000, scheduledTx: map[string]string{}}
	if t != nil {
		e.actors["operator"] = &Actor{
			Name:     "operator",
			ID:       t.OperatorID,
			Key:      t.OperatorKey.PublicKey(),
			SignKeys: []hiero.PrivateKey{t.OperatorKey},
			KeyType:  "operator",
		}
	} else {
		k, _ := hiero.PrivateKeyGenerateEd25519()
		e.actors["operator"] = &Actor{Name: "operator", ID: hiero.AccountID{Account: 2}, Key: k.PublicKey(), SignKeys: []hiero.PrivateKey{k}}
	}
	return e
}

func (e *Env) Planning() bool { return e.Target == nil }

func (e *Env) Client() *hiero.Client {
	if e.Target == nil {
		return nil
	}
	return e.Target.Client
}

func (e *Env) AddActor(a *Actor) error {
	if _, ok := e.actors[a.Name]; ok {
		return fmt.Errorf("name %q is already used", a.Name)
	}
	if _, ok := e.entities[a.Name]; ok {
		return fmt.Errorf("name %q is already used", a.Name)
	}
	e.actors[a.Name] = a
	return nil
}

func (e *Env) Actor(ref string) (*Actor, error) {
	if a, ok := e.actors[ref]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("unknown actor %q", ref)
}

// Actors returns actors in no particular order.
func (e *Env) Actors() map[string]*Actor { return e.actors }

// Account resolves an actor name or a literal 0.0.x id.
func (e *Env) Account(ref string) (hiero.AccountID, error) {
	if a, ok := e.actors[ref]; ok {
		return a.ID, nil
	}
	if isEntityID(ref) {
		return hiero.AccountIDFromString(ref)
	}
	return hiero.AccountID{}, fmt.Errorf("unknown account %q", ref)
}

// PublicKey resolves an actor name to the key it has on chain.
func (e *Env) PublicKey(ref string) (hiero.Key, error) {
	a, err := e.Actor(ref)
	if err != nil {
		return nil, err
	}
	return a.Key, nil
}

func (e *Env) Bind(name, kind, id string) error {
	if name == "" {
		return nil
	}
	if _, ok := e.actors[name]; ok {
		return fmt.Errorf("name %q is already an actor", name)
	}
	if old, ok := e.entities[name]; ok && !e.Planning() && old.id != id {
		return fmt.Errorf("name %q is already bound to %s %s", name, old.kind, old.id)
	}
	e.entities[name] = entity{kind: kind, id: id}
	return nil
}

// Declare reserves a name during planning so later steps can refer to it.
func (e *Env) Declare(name, kind string) error {
	if name == "" {
		return nil
	}
	if _, ok := e.entities[name]; ok {
		return fmt.Errorf("name %q is declared twice", name)
	}
	e.nextFake++
	return e.Bind(name, kind, "0.0."+strconv.FormatInt(e.nextFake, 10))
}

func (e *Env) resolve(ref, kind string) (string, error) {
	if ent, ok := e.entities[ref]; ok {
		if ent.kind != kind {
			return "", fmt.Errorf("%q is a %s, not a %s", ref, ent.kind, kind)
		}
		return ent.id, nil
	}
	if isEntityID(ref) {
		return ref, nil
	}
	return "", fmt.Errorf("unknown %s %q", kind, ref)
}

func (e *Env) Token(ref string) (hiero.TokenID, error) {
	id, err := e.resolve(ref, "token")
	if err != nil {
		return hiero.TokenID{}, err
	}
	return hiero.TokenIDFromString(id)
}

func (e *Env) Topic(ref string) (hiero.TopicID, error) {
	id, err := e.resolve(ref, "topic")
	if err != nil {
		return hiero.TopicID{}, err
	}
	return hiero.TopicIDFromString(id)
}

func (e *Env) Schedule(ref string) (hiero.ScheduleID, error) {
	id, err := e.resolve(ref, "schedule")
	if err != nil {
		return hiero.ScheduleID{}, err
	}
	return hiero.ScheduleIDFromString(id)
}

// ScheduledTx returns the transaction id a named schedule executes as.
func (e *Env) ScheduledTx(name string) (string, bool) {
	id, ok := e.scheduledTx[name]
	return id, ok
}

// Lookup turns any name into its id string, for display and assertions.
func (e *Env) Lookup(ref string) (string, bool) {
	if a, ok := e.actors[ref]; ok {
		return a.ID.String(), true
	}
	if ent, ok := e.entities[ref]; ok {
		return ent.id, true
	}
	if isEntityID(ref) {
		return ref, true
	}
	return "", false
}

// NameOf finds the scenario name for an id, used to make output readable.
func (e *Env) NameOf(id string) string {
	for name, a := range e.actors {
		if a.ID.String() == id {
			return name
		}
	}
	for name, ent := range e.entities {
		if ent.id == id {
			return name
		}
	}
	return ""
}

// SignKeys collects private keys for actor refs.
func (e *Env) SignKeys(refs []string) ([]hiero.PrivateKey, error) {
	var out []hiero.PrivateKey
	for _, r := range refs {
		a, err := e.Actor(r)
		if err != nil {
			return nil, err
		}
		out = append(out, a.SignKeys...)
	}
	return out, nil
}

func isEntityID(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return false
		}
	}
	return true
}
