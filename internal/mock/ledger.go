package mock

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

type code = services.ResponseCodeEnum

const (
	codeOK      = services.ResponseCodeEnum_OK
	codeSuccess = services.ResponseCodeEnum_SUCCESS
)

type snapshot struct {
	at    time.Time
	state *state
}

type ledger struct {
	mu      sync.Mutex
	now     func() time.Time
	last    time.Time
	nextNum int64
	st      *state

	records map[txKey]*record
	history []*record // consensus order

	lag       time.Duration
	snapshots []snapshot

	changed chan struct{} // closed and replaced whenever a topic gets a message
	done    chan struct{}
}

func newLedger(now func() time.Time, lag time.Duration) *ledger {
	return &ledger{
		now:     now,
		nextNum: firstEntity,
		st:      newState(),
		records: map[txKey]*record{},
		lag:     lag,
		changed: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (l *ledger) genesis(operatorKey *services.Key, balance int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	start := l.tick()
	for _, num := range []int64{operatorAccount, nodeAccount, feeAccount} {
		l.st.accounts[num] = &account{
			num:     num,
			key:     operatorKey,
			created: start,
			tokens:  map[int64]*tokenRelation{},
		}
	}
	l.st.accounts[operatorAccount].balance = balance
	if l.lag > 0 {
		l.snapshots = append(l.snapshots, snapshot{at: start, state: l.st.clone()})
	}
}

// tick returns the next consensus time, strictly after the previous one.
func (l *ledger) tick() time.Time {
	t := l.now()
	if !t.After(l.last) {
		t = l.last.Add(time.Nanosecond)
	}
	l.last = t
	return t
}

func (l *ledger) newEntityNum() int64 {
	n := l.nextNum
	l.nextNum++
	return n
}

func (l *ledger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.done:
	default:
		close(l.done)
	}
}

// notify wakes topic subscribers.
func (l *ledger) notify() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *ledger) commit(r *record) {
	l.records[r.id] = r
	l.history = append(l.history, r)
	if l.lag > 0 {
		l.snapshots = append(l.snapshots, snapshot{at: r.consensus, state: l.st.clone()})
	}
}

// view returns the state the mirror may show and the newest consensus time it
// covers. Callers must hold l.mu.
func (l *ledger) view() (*state, time.Time) {
	if l.lag <= 0 {
		return l.st, l.last
	}
	cutoff := l.now().Add(-l.lag)
	keep := 0
	for i, s := range l.snapshots {
		if !s.at.After(cutoff) {
			keep = i
		}
	}
	// older snapshots can never be selected again
	l.snapshots = l.snapshots[keep:]
	return l.snapshots[0].state, l.snapshots[0].at
}

// account resolves an account id, including 20 byte evm aliases. Accounts
// that do not exist return INVALID_ACCOUNT_ID, key aliases are not supported.
func (s *state) account(id *services.AccountID) (*account, code) {
	if id == nil || id.ShardNum != 0 || id.RealmNum != 0 {
		return nil, services.ResponseCodeEnum_INVALID_ACCOUNT_ID
	}
	switch v := id.Account.(type) {
	case *services.AccountID_AccountNum:
		if a, found := s.accounts[v.AccountNum]; found {
			return a, codeOK
		}
		return nil, services.ResponseCodeEnum_INVALID_ACCOUNT_ID
	case *services.AccountID_Alias:
		if len(v.Alias) != 20 {
			return nil, services.ResponseCodeEnum_NOT_SUPPORTED
		}
		if num, found := s.aliases[hex.EncodeToString(v.Alias)]; found {
			return s.accounts[num], codeOK
		}
		// transfers to an unknown evm address would create a hollow account
		return nil, services.ResponseCodeEnum_NOT_SUPPORTED
	}
	return nil, services.ResponseCodeEnum_INVALID_ACCOUNT_ID
}

func (s *state) token(id *services.TokenID) (*token, code) {
	if id == nil || id.ShardNum != 0 || id.RealmNum != 0 {
		return nil, services.ResponseCodeEnum_INVALID_TOKEN_ID
	}
	t, found := s.tokens[id.TokenNum]
	if !found {
		return nil, services.ResponseCodeEnum_INVALID_TOKEN_ID
	}
	if t.deleted {
		return nil, services.ResponseCodeEnum_TOKEN_WAS_DELETED
	}
	return t, codeOK
}

func (s *state) topic(id *services.TopicID) (*topic, code) {
	if id == nil || id.ShardNum != 0 || id.RealmNum != 0 {
		return nil, services.ResponseCodeEnum_INVALID_TOPIC_ID
	}
	t, found := s.topics[id.TopicNum]
	if !found || t.deleted {
		return nil, services.ResponseCodeEnum_INVALID_TOPIC_ID
	}
	return t, codeOK
}

func (s *state) schedule(id *services.ScheduleID) (*schedule, code) {
	if id == nil || id.ShardNum != 0 || id.RealmNum != 0 {
		return nil, services.ResponseCodeEnum_INVALID_SCHEDULE_ID
	}
	sc, found := s.schedules[id.ScheduleNum]
	if !found {
		return nil, services.ResponseCodeEnum_INVALID_SCHEDULE_ID
	}
	return sc, codeOK
}

// liveAccount is account plus the deleted check most transactions need.
func (s *state) liveAccount(id *services.AccountID) (*account, code) {
	a, c := s.account(id)
	if c != codeOK {
		return nil, c
	}
	if a.deleted {
		return nil, services.ResponseCodeEnum_ACCOUNT_DELETED
	}
	return a, codeOK
}
