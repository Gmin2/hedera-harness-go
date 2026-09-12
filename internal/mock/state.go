package mock

import (
	"maps"
	"slices"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

type account struct {
	num                  int64
	key                  *services.Key
	balance              int64
	alias                []byte // 20 byte evm address, nil when the account has none
	memo                 string
	maxAutoAssociations  int32
	usedAutoAssociations int32
	receiverSigRequired  bool
	deleted              bool
	created              time.Time
	tokens               map[int64]*tokenRelation
}

type tokenRelation struct {
	balance   int64 // fungible units, or number of serials owned for nfts
	kyc       services.TokenKycStatus
	freeze    services.TokenFreezeStatus
	automatic bool
	created   time.Time
}

type token struct {
	num        int64
	spec       *services.TokenCreateTransactionBody // immutable, token update is not supported
	supply     int64
	treasury   int64
	paused     bool
	deleted    bool
	fees       []*services.CustomFee
	nfts       map[int64]*nft
	lastSerial int64
	created    time.Time
}

func (t *token) fungible() bool { return t.spec.TokenType == services.TokenType_FUNGIBLE_COMMON }

type nft struct {
	serial   int64
	owner    int64
	metadata []byte
	created  time.Time
	modified time.Time
}

type topic struct {
	num         int64
	memo        string
	adminKey    *services.Key
	submitKey   *services.Key
	autoRenew   int64
	deleted     bool
	runningHash []byte
	messages    []*topicMessage
	created     time.Time
}

type topicMessage struct {
	topic       int64
	sequence    uint64
	consensus   time.Time
	message     []byte
	runningHash []byte
	payer       int64
	chunk       *services.ConsensusMessageChunkInfo
}

type scheduleSignature struct {
	signature
	consensus time.Time
}

type schedule struct {
	num           int64
	creator       int64
	payer         int64
	adminKey      *services.Key
	memo          string
	body          *services.SchedulableTransactionBody
	identity      string // bytes of the create body, used to find identical schedules
	expiration    time.Time
	waitForExpiry bool
	signatures    []scheduleSignature
	scheduledTx   txKey
	created       time.Time
	executed      time.Time
	deleted       time.Time
}

func (s *schedule) pending() bool { return s.executed.IsZero() && s.deleted.IsZero() }

func (s *schedule) signatories() sigSet {
	set := sigSet{}
	for _, sig := range s.signatures {
		id, _ := primitiveID(sig.key)
		set[id] = sig.signature
	}
	return set
}

type airdropKey struct {
	sender, receiver, token, serial int64 // serial is 0 for fungible airdrops
}

type pendingAirdrop struct {
	airdropKey
	amount  int64
	created time.Time
}

// state is everything the ledger knows about entities. It is kept apart from
// the ledger bookkeeping so the mirror can hold older copies when lagging.
type state struct {
	accounts  map[int64]*account
	aliases   map[string]int64 // hex evm address -> account
	tokens    map[int64]*token
	topics    map[int64]*topic
	schedules map[int64]*schedule
	airdrops  map[airdropKey]*pendingAirdrop
}

func newState() *state {
	return &state{
		accounts:  map[int64]*account{},
		aliases:   map[string]int64{},
		tokens:    map[int64]*token{},
		topics:    map[int64]*topic{},
		schedules: map[int64]*schedule{},
		airdrops:  map[airdropKey]*pendingAirdrop{},
	}
}

// clone copies every mutable entity. Protobuf values held by entities are
// never mutated after they are stored, so they are shared.
func (s *state) clone() *state {
	c := newState()
	for num, a := range s.accounts {
		cp := *a
		cp.tokens = make(map[int64]*tokenRelation, len(a.tokens))
		for id, rel := range a.tokens {
			r := *rel
			cp.tokens[id] = &r
		}
		c.accounts[num] = &cp
	}
	c.aliases = maps.Clone(s.aliases)
	for num, t := range s.tokens {
		cp := *t
		cp.nfts = make(map[int64]*nft, len(t.nfts))
		for serial, n := range t.nfts {
			nc := *n
			cp.nfts[serial] = &nc
		}
		c.tokens[num] = &cp
	}
	for num, t := range s.topics {
		cp := *t
		cp.messages = slices.Clone(t.messages)
		c.topics[num] = &cp
	}
	for num, sc := range s.schedules {
		cp := *sc
		cp.signatures = slices.Clone(sc.signatures)
		c.schedules[num] = &cp
	}
	for k, a := range s.airdrops {
		cp := *a
		c.airdrops[k] = &cp
	}
	return c
}

// record is what the node remembers about one applied transaction. It backs
// receipts, records and mirror transactions.
type record struct {
	id               txKey
	name             string
	consensus        time.Time
	memo             string
	maxFee           uint64
	receipt          *services.TransactionReceipt
	transfers        []*services.AccountAmount
	tokenTransfers   []*services.TokenTransferList
	assessedFees     []*services.AssessedCustomFee
	autoAssociations []*services.TokenAssociation
	pendingAirdrops  []*services.PendingAirdropRecord
	entity           int64
	scheduleRef      int64
}

func (r *record) proto() *services.TransactionRecord {
	rec := &services.TransactionRecord{
		Receipt:                    r.receipt,
		ConsensusTimestamp:         timestampProto(r.consensus),
		TransactionID:              r.id.proto(),
		Memo:                       r.memo,
		TokenTransferLists:         r.tokenTransfers,
		AssessedCustomFees:         r.assessedFees,
		AutomaticTokenAssociations: r.autoAssociations,
		NewPendingAirdrops:         r.pendingAirdrops,
	}
	if len(r.transfers) > 0 {
		rec.TransferList = &services.TransferList{AccountAmounts: r.transfers}
	}
	if r.scheduleRef != 0 {
		rec.ScheduleRef = scheduleProto(r.scheduleRef)
	}
	return rec
}
