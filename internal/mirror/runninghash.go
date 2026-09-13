package mirror

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// RunningHashVersion is the topic running hash layout this package computes.
const RunningHashVersion = 3

// Consensus nodes build the v3 running hash by writing the fields through a
// java ObjectOutputStream, so the hashed bytes carry its stream header,
// byte array class descriptor, block data marker and a back reference.
var (
	javaStreamHeader     = []byte{0xac, 0xed, 0x00, 0x05}
	javaByteArrayClass   = []byte{0x75, 0x72, 0x00, 0x02, 0x5b, 0x42, 0xac, 0xf3, 0x17, 0xf8, 0x06, 0x08, 0x54, 0xe0, 0x02, 0x00, 0x00, 0x78, 0x70}
	javaByteArrayBackRef = []byte{0x75, 0x71, 0x00, 0x7e, 0x00, 0x00}
)

// RunningHashInput is one topic message as the running hash sees it.
type RunningHashInput struct {
	Previous []byte // 48 zero bytes before the first message
	Payer    [3]int64
	Topic    [3]int64
	Seconds  int64
	Nanos    int32
	Sequence int64
	Message  []byte
}

// NextRunningHash computes the v3 running hash after one message.
func NextRunningHash(in RunningHashInput) []byte {
	prev := in.Previous
	if len(prev) == 0 {
		prev = make([]byte, sha512.Size384)
	}

	var primitives []byte
	i64 := func(v int64) { primitives = binary.BigEndian.AppendUint64(primitives, uint64(v)) }
	i64(RunningHashVersion)
	for _, v := range in.Payer {
		i64(v)
	}
	for _, v := range in.Topic {
		i64(v)
	}
	i64(in.Seconds)
	primitives = binary.BigEndian.AppendUint32(primitives, uint32(in.Nanos))
	i64(in.Sequence)

	msgDigest := sha512.Sum384(in.Message)

	var b []byte
	b = append(b, javaStreamHeader...)
	b = append(b, javaByteArrayClass...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(prev)))
	b = append(b, prev...)
	b = append(b, 0x77, byte(len(primitives)))
	b = append(b, primitives...)
	b = append(b, javaByteArrayBackRef...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(msgDigest)))
	b = append(b, msgDigest[:]...)

	sum := sha512.Sum384(b)
	return sum[:]
}

// VerifyChain recomputes the running hash of every message, which must start
// at sequence 1, and reports the first sequence number that does not match.
func VerifyChain(topicID string, msgs []TopicMessage) (int64, error) {
	topic, err := entityParts(topicID)
	if err != nil {
		return 0, err
	}
	prev := make([]byte, sha512.Size384)
	for i, m := range msgs {
		if m.SequenceNumber != int64(i)+1 {
			return m.SequenceNumber, fmt.Errorf("expected sequence %d, got %d", i+1, m.SequenceNumber)
		}
		if m.RunningHashVersion != RunningHashVersion {
			return m.SequenceNumber, fmt.Errorf("running hash version %d is not supported", m.RunningHashVersion)
		}
		payer, err := entityParts(m.PayerAccountID)
		if err != nil {
			return m.SequenceNumber, err
		}
		secs, nanos, err := splitTimestamp(m.ConsensusTimestamp)
		if err != nil {
			return m.SequenceNumber, err
		}
		body, err := base64.StdEncoding.DecodeString(m.Message)
		if err != nil {
			return m.SequenceNumber, fmt.Errorf("message is not base64: %w", err)
		}
		want, err := base64.StdEncoding.DecodeString(m.RunningHash)
		if err != nil {
			return m.SequenceNumber, fmt.Errorf("running hash is not base64: %w", err)
		}
		got := NextRunningHash(RunningHashInput{
			Previous: prev, Payer: payer, Topic: topic,
			Seconds: secs, Nanos: nanos, Sequence: m.SequenceNumber, Message: body,
		})
		if string(got) != string(want) {
			return m.SequenceNumber, fmt.Errorf("running hash mismatch")
		}
		prev = want
	}
	return 0, nil
}

func entityParts(id string) ([3]int64, error) {
	var out [3]int64
	parts := strings.Split(id, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("bad entity id %q", id)
	}
	for i, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return out, fmt.Errorf("bad entity id %q", id)
		}
		out[i] = n
	}
	return out, nil
}

func splitTimestamp(ts string) (int64, int32, error) {
	secs, nanos, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad timestamp %q", ts)
	}
	nanos = (nanos + "000000000")[:9]
	n, err := strconv.ParseInt(nanos, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("bad timestamp %q", ts)
	}
	return s, int32(n), nil
}
