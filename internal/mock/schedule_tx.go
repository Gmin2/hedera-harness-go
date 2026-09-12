package mock

import (
	"maps"
	"slices"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	"google.golang.org/protobuf/proto"
)

const (
	defaultScheduleLifetime = 30 * time.Minute
	maxScheduleLifetime     = 62 * 24 * time.Hour
)

func (l *ledger) createSchedule(t *txn, b *services.ScheduleCreateTransactionBody) code {
	if b.ScheduledTransactionBody == nil {
		return services.ResponseCodeEnum_INVALID_TRANSACTION
	}
	if _, supported := scheduledBody(b.ScheduledTransactionBody); !supported {
		return services.ResponseCodeEnum_NOT_SUPPORTED
	}
	if len(b.Memo) > maxMemoBytes {
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	}
	if b.AdminKey != nil && !validKey(b.AdminKey) {
		return services.ResponseCodeEnum_BAD_ENCODING
	}

	payer := t.payer
	if b.PayerAccountID != nil {
		a, c := l.st.liveAccount(b.PayerAccountID)
		if c != codeOK {
			return services.ResponseCodeEnum_INVALID_SCHEDULE_PAYER_ID
		}
		payer = a.num
	}

	expiration := t.rec.consensus.Add(defaultScheduleLifetime)
	if b.ExpirationTime != nil {
		expiration = timeFromProto(b.ExpirationTime)
		switch {
		case !expiration.After(t.rec.consensus):
			return services.ResponseCodeEnum_SCHEDULE_EXPIRATION_TIME_MUST_BE_HIGHER_THAN_CONSENSUS_TIME
		case expiration.Sub(t.rec.consensus) > maxScheduleLifetime:
			return services.ResponseCodeEnum_SCHEDULE_EXPIRATION_TIME_TOO_FAR_IN_FUTURE
		}
	}

	identity, err := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if err != nil {
		return services.ResponseCodeEnum_INVALID_TRANSACTION_BODY
	}
	for _, num := range slices.Sorted(maps.Keys(l.st.schedules)) {
		existing := l.st.schedules[num]
		if existing.pending() && existing.identity == string(identity) {
			t.rec.receipt.ScheduleID = scheduleProto(existing.num)
			t.rec.receipt.ScheduledTransactionID = existing.scheduledTx.proto()
			return services.ResponseCodeEnum_IDENTICAL_SCHEDULE_ALREADY_CREATED
		}
	}

	scheduledTx := t.id
	scheduledTx.scheduled = true
	sc := &schedule{
		num:           l.newEntityNum(),
		creator:       t.payer,
		payer:         payer,
		adminKey:      b.AdminKey,
		memo:          b.Memo,
		body:          b.ScheduledTransactionBody,
		identity:      string(identity),
		expiration:    expiration,
		waitForExpiry: b.WaitForExpiry,
		scheduledTx:   scheduledTx,
		created:       t.rec.consensus,
	}
	required, c := l.scheduleKeys(sc)
	if c != codeOK {
		return services.ResponseCodeEnum_UNRESOLVABLE_REQUIRED_SIGNERS
	}
	sc.addSignatures(t.sigs, required, t.rec.consensus)
	l.st.schedules[sc.num] = sc

	t.rec.receipt.ScheduleID = scheduleProto(sc.num)
	t.rec.receipt.ScheduledTransactionID = sc.scheduledTx.proto()
	t.rec.entity = sc.num
	l.executeWhenReady(t, sc, required)
	return codeOK
}

func (l *ledger) signSchedule(t *txn, b *services.ScheduleSignTransactionBody) code {
	sc, c := l.st.schedule(b.ScheduleID)
	if c != codeOK {
		return c
	}
	if !sc.pending() || t.rec.consensus.After(sc.expiration) {
		return services.ResponseCodeEnum_INVALID_SCHEDULE_ID
	}
	required, c := l.scheduleKeys(sc)
	if c != codeOK {
		return services.ResponseCodeEnum_UNRESOLVABLE_REQUIRED_SIGNERS
	}
	if sc.addSignatures(t.sigs, required, t.rec.consensus) == 0 {
		return services.ResponseCodeEnum_NO_NEW_VALID_SIGNATURES
	}
	t.rec.receipt.ScheduledTransactionID = sc.scheduledTx.proto()
	t.rec.entity = sc.num
	l.executeWhenReady(t, sc, required)
	return codeOK
}

func (l *ledger) deleteScheduleKeys(b *services.ScheduleDeleteTransactionBody) ([]*services.Key, code) {
	sc, c := l.st.schedule(b.ScheduleID)
	if c != codeOK {
		return nil, c
	}
	if sc.adminKey == nil {
		return nil, services.ResponseCodeEnum_SCHEDULE_IS_IMMUTABLE
	}
	return []*services.Key{sc.adminKey}, codeOK
}

func (l *ledger) deleteSchedule(t *txn, b *services.ScheduleDeleteTransactionBody) code {
	sc, c := l.st.schedule(b.ScheduleID)
	if c != codeOK {
		return c
	}
	switch {
	case !sc.executed.IsZero():
		return services.ResponseCodeEnum_SCHEDULE_ALREADY_EXECUTED
	case !sc.deleted.IsZero():
		return services.ResponseCodeEnum_SCHEDULE_ALREADY_DELETED
	}
	sc.deleted = t.rec.consensus
	t.rec.entity = sc.num
	return codeOK
}

// scheduleKeys is the full signer requirement of the scheduled body: the
// schedule payer plus whatever the inner transaction type needs.
func (l *ledger) scheduleKeys(sc *schedule) ([]*services.Key, code) {
	inner := l.scheduledTxn(sc)
	op, _ := l.operation(inner)
	return l.requiredKeys(inner, op)
}

func (l *ledger) scheduledTxn(sc *schedule) *txn {
	body, _ := scheduledBody(sc.body)
	body.TransactionID = sc.scheduledTx.proto()
	return &txn{body: body, id: sc.scheduledTx, payer: sc.payer, sigs: sc.signatories(), schedule: sc}
}

// addSignatures keeps the verified signatures that belong to a required key
// and were not collected before. It returns how many were added.
func (sc *schedule) addSignatures(sigs sigSet, required []*services.Key, at time.Time) int {
	relevant := map[string]bool{}
	for _, k := range required {
		collectPrimitives(k, relevant)
	}
	have := sc.signatories()
	added := 0
	for _, id := range slices.Sorted(maps.Keys(sigs)) {
		if _, dup := have[id]; dup || !relevant[id] {
			continue
		}
		sc.signatures = append(sc.signatures, scheduleSignature{signature: sigs[id], consensus: at})
		added++
	}
	return added
}

func (l *ledger) executeWhenReady(t *txn, sc *schedule, required []*services.Key) {
	if sc.waitForExpiry {
		return
	}
	have := sc.signatories()
	for _, k := range required {
		if !have.satisfies(k) {
			return
		}
	}
	t.after = append(t.after, func() {
		inner := l.scheduledTxn(sc)
		op, _ := l.operation(inner)
		l.execute(inner, op)
	})
}
