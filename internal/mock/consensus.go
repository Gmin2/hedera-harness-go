package mock

import (
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	"google.golang.org/protobuf/proto"
)

const (
	maxClockSkew     = 10 * time.Second
	maxValidDuration = 180
	maxMemoBytes     = 100
)

// txn is a transaction on its way through consensus. For scheduled
// executions sigs are the schedule signatories and schedule is set.
type txn struct {
	body     *services.TransactionBody
	id       txKey
	payer    int64
	sigs     sigSet
	rec      *record
	schedule *schedule
	after    []func() // runs once the record is committed
}

// operation is how one transaction type plugs into consensus. keys lists
// the keys that must sign on top of the payer, apply mutates the state and
// must leave it untouched when it returns a failure.
type operation struct {
	name  string
	keys  func() ([]*services.Key, code)
	apply func() code
}

// submit runs the node prechecks and, when they pass, applies the
// transaction synchronously. The returned code is the precheck result.
func (l *ledger) submit(tx *services.Transaction) code {
	bodyBytes, sigMap := tx.GetBodyBytes(), tx.GetSigMap()
	if len(tx.GetSignedTransactionBytes()) > 0 {
		var signed services.SignedTransaction
		if err := proto.Unmarshal(tx.SignedTransactionBytes, &signed); err != nil {
			return services.ResponseCodeEnum_INVALID_TRANSACTION_BODY
		}
		bodyBytes, sigMap = signed.BodyBytes, signed.SigMap
	}
	body := new(services.TransactionBody)
	if len(bodyBytes) == 0 || proto.Unmarshal(bodyBytes, body) != nil {
		return services.ResponseCodeEnum_INVALID_TRANSACTION_BODY
	}

	id := body.GetTransactionID()
	if id.GetTransactionValidStart() == nil || id.GetAccountID() == nil {
		return services.ResponseCodeEnum_INVALID_TRANSACTION_ID
	}
	if id.Scheduled || id.Nonce != 0 {
		return services.ResponseCodeEnum_TRANSACTION_ID_FIELD_NOT_ALLOWED
	}
	node := body.GetNodeAccountID()
	if node.GetShardNum() != 0 || node.GetRealmNum() != 0 || node.GetAccountNum() != nodeAccount {
		return services.ResponseCodeEnum_INVALID_NODE_ACCOUNT
	}
	if body.BatchKey != nil {
		return services.ResponseCodeEnum_NOT_SUPPORTED
	}
	if len(body.Memo) > maxMemoBytes {
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, isNum := id.AccountID.Account.(*services.AccountID_AccountNum); !isNum {
		return services.ResponseCodeEnum_PAYER_ACCOUNT_NOT_FOUND
	}
	payer, c := l.st.account(id.AccountID)
	if c != codeOK {
		return services.ResponseCodeEnum_PAYER_ACCOUNT_NOT_FOUND
	}
	if payer.deleted {
		return services.ResponseCodeEnum_PAYER_ACCOUNT_DELETED
	}

	key := keyOfTx(id)
	if _, dup := l.records[key]; dup {
		return services.ResponseCodeEnum_DUPLICATE_TRANSACTION
	}

	now := l.now()
	start := timeFromProto(id.TransactionValidStart)
	duration := body.GetTransactionValidDuration().GetSeconds()
	if duration <= 0 || duration > maxValidDuration {
		return services.ResponseCodeEnum_INVALID_TRANSACTION_DURATION
	}
	if start.After(now.Add(maxClockSkew)) {
		return services.ResponseCodeEnum_INVALID_TRANSACTION_START
	}
	if start.Add(time.Duration(duration) * time.Second).Before(now) {
		return services.ResponseCodeEnum_TRANSACTION_EXPIRED
	}

	sigs := verifySignatures(bodyBytes, sigMap)
	if !sigs.satisfies(payer.key) {
		return services.ResponseCodeEnum_INVALID_SIGNATURE
	}

	t := &txn{body: body, id: key, payer: payer.num, sigs: sigs}
	op, supported := l.operation(t)
	if !supported {
		return services.ResponseCodeEnum_NOT_SUPPORTED
	}
	l.execute(t, op)
	return codeOK
}

// execute reaches consensus on t: it checks the required signatures, applies
// the operation and commits a record whatever the outcome.
func (l *ledger) execute(t *txn, op operation) {
	t.rec = &record{
		id:        t.id,
		name:      op.name,
		consensus: l.tick(),
		memo:      t.body.Memo,
		maxFee:    t.body.TransactionFee,
		receipt:   &services.TransactionReceipt{},
	}
	if t.schedule != nil {
		// a schedule counts as executed whatever the inner outcome is
		t.schedule.executed = t.rec.consensus
		t.rec.scheduleRef = t.schedule.num
	}

	status := l.checkKeys(t, op)
	if status == codeOK {
		status = op.apply()
	}
	if status == codeOK {
		status = codeSuccess
	}
	t.rec.receipt.Status = status
	if status != codeSuccess {
		t.rec.transfers = nil
		t.rec.tokenTransfers = nil
		t.rec.assessedFees = nil
		t.rec.autoAssociations = nil
		t.rec.pendingAirdrops = nil
	}

	l.commit(t.rec)
	for _, fn := range t.after {
		fn()
	}
}

func (l *ledger) checkKeys(t *txn, op operation) code {
	keys, c := l.requiredKeys(t, op)
	if c != codeOK {
		return c
	}
	for _, k := range keys {
		if !t.sigs.satisfies(k) {
			return services.ResponseCodeEnum_INVALID_SIGNATURE
		}
	}
	return codeOK
}

func (l *ledger) requiredKeys(t *txn, op operation) ([]*services.Key, code) {
	var keys []*services.Key
	if t.schedule != nil {
		// a scheduled payer never signed the outer transaction
		payer, c := l.st.liveAccount(accountProto(t.payer))
		if c != codeOK {
			return nil, c
		}
		keys = append(keys, payer.key)
	}
	if op.keys == nil {
		return keys, codeOK
	}
	more, c := op.keys()
	if c != codeOK {
		return nil, c
	}
	for _, k := range more {
		if k != nil {
			keys = append(keys, k)
		}
	}
	return keys, codeOK
}
