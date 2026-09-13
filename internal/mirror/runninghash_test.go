package mirror

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/topic-running-hash.json is the first three messages of testnet
// topic 0.0.4320226, fetched from the public mirror node unmodified.
func TestVerifyChainOnRealTestnetMessages(t *testing.T) {
	raw, err := os.ReadFile("testdata/topic-running-hash.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		TopicID  string         `json:"topic_id"`
		Messages []TopicMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if at, err := VerifyChain(fx.TopicID, fx.Messages); err != nil {
		t.Fatalf("real chain should verify, broke at %d: %v", at, err)
	}

	tampered := append([]TopicMessage(nil), fx.Messages...)
	tampered[1].Message = tampered[0].Message
	if at, err := VerifyChain(fx.TopicID, tampered); err == nil || at != 2 {
		t.Fatalf("tampered message should break at 2, got %d %v", at, err)
	}
	if at, err := VerifyChain(fx.TopicID, fx.Messages[1:]); err == nil || at != 2 {
		t.Fatalf("a chain that skips message 1 should fail, got %d %v", at, err)
	}
}
