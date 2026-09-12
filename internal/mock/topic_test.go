package mock

import (
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"google.golang.org/grpc/status"
)

func (h *harness) submit(topic hiero.TopicID, message string, keys ...hiero.PrivateKey) (hiero.TransactionReceipt, hiero.Status) {
	h.t.Helper()
	tx := hiero.NewTopicMessageSubmitTransaction().SetTopicID(topic).SetMessage([]byte(message))
	resp, err := signed(h, tx, keys...)
	// the sdk fetches receipts for submit transactions itself, so a
	// consensus failure can already surface from Execute
	var failed hiero.ErrHederaReceiptStatus
	if errors.As(err, &failed) {
		return hiero.TransactionReceipt{}, failed.Status
	}
	if err != nil {
		h.t.Fatal(err)
	}
	receipt, err := resp.SetValidateStatus(false).GetReceipt(h.client)
	if err != nil {
		h.t.Fatal(err)
	}
	return receipt, receipt.Status
}

func TestTopicSubmitKey(t *testing.T) {
	h := newHarness(t)
	submitKey, _ := hiero.PrivateKeyGenerateEcdsa()
	stranger, _ := hiero.PrivateKeyGenerateEd25519()

	topic := *h.run(hiero.NewTopicCreateTransaction().
		SetSubmitKey(submitKey.PublicKey()).
		SetTopicMemo("private").
		Execute(h.client)).TopicID

	_, st := h.submit(topic, "sneaky", stranger)
	expectStatus(t, st, hiero.StatusInvalidSignature)

	first, st := h.submit(topic, "hello", submitKey)
	expectStatus(t, st, hiero.StatusSuccess)
	second, st := h.submit(topic, "world", submitKey)
	expectStatus(t, st, hiero.StatusSuccess)
	if first.TopicSequenceNumber != 1 || second.TopicSequenceNumber != 2 || len(second.TopicRunningHash) != 48 {
		t.Fatalf("sequence numbers %d %d hash %x", first.TopicSequenceNumber, second.TopicSequenceNumber, second.TopicRunningHash)
	}

	_, st = h.submit(hiero.TopicID{Topic: 99999}, "nowhere")
	expectStatus(t, st, hiero.StatusInvalidTopicID)

	info, err := hiero.NewTopicInfoQuery().SetTopicID(topic).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if info.SequenceNumber != 2 || info.TopicMemo != "private" {
		t.Fatalf("topic info %+v", info)
	}

	var tp mirror.Topic
	h.mustRest("/topics/"+topic.String(), &tp)
	if tp.SubmitKey == nil || tp.SubmitKey.Type != "ECDSA_SECP256K1" || tp.Memo != "private" {
		t.Fatalf("mirror topic %+v", tp)
	}

	var msgs mirror.TopicMessages
	h.mustRest("/topics/"+topic.String()+"/messages?sequencenumber=gt:1", &msgs)
	if len(msgs.Messages) != 1 || msgs.Messages[0].SequenceNumber != 2 {
		t.Fatalf("messages after 1 %+v", msgs)
	}
	var msg mirror.TopicMessage
	h.mustRest("/topics/"+topic.String()+"/messages/1", &msg)
	if raw, _ := base64.StdEncoding.DecodeString(msg.Message); string(raw) != "hello" || msg.RunningHashVersion != 3 {
		t.Fatalf("message 1 %+v", msg)
	}
	var desc mirror.TopicMessages
	h.mustRest("/topics/"+topic.String()+"/messages?order=desc&limit=1", &desc)
	if len(desc.Messages) != 1 || desc.Messages[0].SequenceNumber != 2 || desc.Links.Next == nil {
		t.Fatalf("latest message page %+v", desc)
	}
}

func TestSubscribeTopic(t *testing.T) {
	h := newHarness(t)
	topic := *h.run(hiero.NewTopicCreateTransaction().Execute(h.client)).TopicID
	h.submit(topic, "before")

	var mu sync.Mutex
	var got []hiero.TopicMessage
	handle, err := hiero.NewTopicMessageQuery().
		SetTopicID(topic).
		SetStartTime(time.Unix(0, 0)).
		SetErrorHandler(func(s status.Status) { t.Errorf("subscription error: %v", s.Message()) }).
		SetCompletionHandler(func() {}).
		Subscribe(h.client, func(m hiero.TopicMessage) {
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
		})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Unsubscribe()

	h.submit(topic, "after")
	waitFor(t, "two topic messages", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	})

	mu.Lock()
	defer mu.Unlock()
	if string(got[0].Contents) != "before" || string(got[1].Contents) != "after" || got[1].SequenceNumber != 2 {
		t.Fatalf("received %q %q seq %d", got[0].Contents, got[1].Contents, got[1].SequenceNumber)
	}
}
