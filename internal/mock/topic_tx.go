package mock

import (
	"crypto/sha512"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
)

const (
	maxMessageBytes    = 1024
	runningHashVersion = 3
)

func (l *ledger) createTopicKeys(b *services.ConsensusCreateTopicTransactionBody) ([]*services.Key, code) {
	keys := []*services.Key{b.AdminKey}
	if b.AutoRenewAccount != nil {
		a, c := l.st.account(b.AutoRenewAccount)
		if c != codeOK {
			return nil, services.ResponseCodeEnum_INVALID_AUTORENEW_ACCOUNT
		}
		keys = append(keys, a.key)
	}
	return keys, codeOK
}

func (l *ledger) createTopic(t *txn, b *services.ConsensusCreateTopicTransactionBody) code {
	if len(b.Memo) > maxMemoBytes {
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	}
	if b.AdminKey != nil && !validKey(b.AdminKey) {
		return services.ResponseCodeEnum_BAD_ENCODING
	}
	if b.SubmitKey != nil && !validKey(b.SubmitKey) {
		return services.ResponseCodeEnum_BAD_ENCODING
	}
	if len(b.CustomFees) > 0 || len(b.FeeExemptKeyList) > 0 || b.FeeScheduleKey != nil {
		return services.ResponseCodeEnum_NOT_SUPPORTED
	}
	tp := &topic{
		num:         l.newEntityNum(),
		memo:        b.Memo,
		adminKey:    b.AdminKey,
		submitKey:   b.SubmitKey,
		autoRenew:   b.AutoRenewAccount.GetAccountNum(),
		runningHash: make([]byte, sha512.Size384),
		created:     t.rec.consensus,
	}
	l.st.topics[tp.num] = tp
	t.rec.receipt.TopicID = topicProto(tp.num)
	t.rec.entity = tp.num
	return codeOK
}

func (l *ledger) submitMessageKeys(b *services.ConsensusSubmitMessageTransactionBody) ([]*services.Key, code) {
	tp, c := l.st.topic(b.TopicID)
	if c != codeOK {
		return nil, c
	}
	return []*services.Key{tp.submitKey}, codeOK
}

func (l *ledger) submitMessage(t *txn, b *services.ConsensusSubmitMessageTransactionBody) code {
	tp, c := l.st.topic(b.TopicID)
	if c != codeOK {
		return c
	}
	switch {
	case len(b.Message) == 0:
		return services.ResponseCodeEnum_INVALID_TOPIC_MESSAGE
	case len(b.Message) > maxMessageBytes:
		return services.ResponseCodeEnum_MESSAGE_SIZE_TOO_LARGE
	}
	if ci := b.ChunkInfo; ci != nil {
		if ci.Total < 1 || ci.Number < 1 || ci.Number > ci.Total {
			return services.ResponseCodeEnum_INVALID_CHUNK_NUMBER
		}
		if ci.InitialTransactionID == nil {
			return services.ResponseCodeEnum_INVALID_CHUNK_TRANSACTION_ID
		}
	}

	msg := &topicMessage{
		topic:     tp.num,
		sequence:  uint64(len(tp.messages)) + 1,
		consensus: t.rec.consensus,
		message:   b.Message,
		payer:     t.payer,
		chunk:     b.ChunkInfo,
	}
	msg.runningHash = nextRunningHash(tp.runningHash, tp.num, msg)
	tp.runningHash = msg.runningHash
	tp.messages = append(tp.messages, msg)

	t.rec.receipt.TopicSequenceNumber = msg.sequence
	t.rec.receipt.TopicRunningHash = msg.runningHash
	t.rec.receipt.TopicRunningHashVersion = runningHashVersion
	t.rec.entity = tp.num
	t.after = append(t.after, l.notify)
	return codeOK
}

// nextRunningHash is the real v3 running hash, so chains built on the mock
// verify the same way as chains read from testnet.
func nextRunningHash(prev []byte, topicNum int64, m *topicMessage) []byte {
	return mirror.NextRunningHash(mirror.RunningHashInput{
		Previous: prev,
		Payer:    [3]int64{0, 0, m.payer},
		Topic:    [3]int64{0, 0, topicNum},
		Seconds:  m.consensus.Unix(),
		Nanos:    int32(m.consensus.Nanosecond()),
		Sequence: int64(m.sequence),
		Message:  m.message,
	})
}
