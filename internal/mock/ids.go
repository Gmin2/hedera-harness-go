package mock

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

func accountProto(num int64) *services.AccountID {
	return &services.AccountID{Account: &services.AccountID_AccountNum{AccountNum: num}}
}

func tokenProto(num int64) *services.TokenID       { return &services.TokenID{TokenNum: num} }
func topicProto(num int64) *services.TopicID       { return &services.TopicID{TopicNum: num} }
func scheduleProto(num int64) *services.ScheduleID { return &services.ScheduleID{ScheduleNum: num} }

func entityString(num int64) string { return "0.0." + strconv.FormatInt(num, 10) }

// parseEntity accepts "0.0.123" or a bare "123". Only shard 0 realm 0 exists.
func parseEntity(s string) (int64, bool) {
	if rest, ok := strings.CutPrefix(s, "0.0."); ok {
		s = rest
	} else if strings.Count(s, ".") > 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func timestampProto(t time.Time) *services.Timestamp {
	return &services.Timestamp{Seconds: t.Unix(), Nanos: int32(t.Nanosecond())}
}

func timeFromProto(ts *services.Timestamp) time.Time {
	return time.Unix(ts.GetSeconds(), int64(ts.GetNanos()))
}

// mirrorTime renders the mirror "seconds.nanoseconds" form.
func mirrorTime(t time.Time) string {
	return fmt.Sprintf("%d.%09d", t.Unix(), t.Nanosecond())
}

func mirrorTimePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := mirrorTime(t)
	return &s
}

// txKey identifies a transaction the way the network does: payer, valid
// start, and the scheduled flag and nonce that children share with parents.
type txKey struct {
	payer     int64
	seconds   int64
	nanos     int32
	scheduled bool
	nonce     int32
}

func keyOfTx(id *services.TransactionID) txKey {
	return txKey{
		payer:     id.GetAccountID().GetAccountNum(),
		seconds:   id.GetTransactionValidStart().GetSeconds(),
		nanos:     id.GetTransactionValidStart().GetNanos(),
		scheduled: id.GetScheduled(),
		nonce:     id.GetNonce(),
	}
}

func (k txKey) mirrorID() string {
	return fmt.Sprintf("0.0.%d-%d-%09d", k.payer, k.seconds, k.nanos)
}

func (k txKey) proto() *services.TransactionID {
	return &services.TransactionID{
		AccountID:             accountProto(k.payer),
		TransactionValidStart: &services.Timestamp{Seconds: k.seconds, Nanos: k.nanos},
		Scheduled:             k.scheduled,
		Nonce:                 k.nonce,
	}
}

// parseMirrorTxID parses 0.0.2-1757712000-000012345.
func parseMirrorTxID(s string) (txKey, bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return txKey{}, false
	}
	payer, ok := parseEntity(parts[0])
	if !ok || !strings.HasPrefix(parts[0], "0.0.") {
		return txKey{}, false
	}
	secs, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || len(parts[1]) > 19 {
		return txKey{}, false
	}
	nanos, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || len(parts[2]) > 9 || nanos < 0 {
		return txKey{}, false
	}
	return txKey{payer: payer, seconds: secs, nanos: int32(nanos)}, true
}
