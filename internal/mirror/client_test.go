package mirror

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTxPath(t *testing.T) {
	cases := []struct {
		in, path, query string
	}{
		{"0.0.5@1757712000.123456789", "0.0.5-1757712000-123456789", ""},
		{"0.0.5@1757712000.1234", "0.0.5-1757712000-000001234", ""},
		{"0.0.5@1757712000.000000007?scheduled", "0.0.5-1757712000-000000007", "scheduled=true"},
		{"0.0.2252@1640075693.891386528/1", "0.0.2252-1640075693-891386528", "nonce=1"},
		{"0.0.7@1640075693.000000001?scheduled/2", "0.0.7-1640075693-000000001", "nonce=2&scheduled=true"},
	}
	for _, c := range cases {
		path, q, err := TxPath(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if path != c.path || q.Encode() != c.query {
			t.Errorf("%s: got %s ?%s", c.in, path, q.Encode())
		}
	}
	if _, _, err := TxPath("0.0.5"); err == nil {
		t.Error("expected error for id without @")
	}
}

func TestPagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/topics/0.0.9/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`{"messages":[{"sequence_number":2,"message":"Yg=="}],"links":{"next":null}}`))
			return
		}
		w.Write([]byte(`{"messages":[{"sequence_number":1,"message":"YQ=="}],"links":{"next":"/api/v1/topics/0.0.9/messages?page=2"}}`))
	})
	mux.HandleFunc("/api/v1/tokens/0.0.404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"_status":{"messages":[{"message":"Not found"}]}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL + "/api/v1")
	msgs, _, err := c.TopicMessages(context.Background(), "0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].SequenceNumber != 2 {
		t.Fatalf("got %+v", msgs)
	}
	if _, _, err := c.Token(context.Background(), "0.0.404"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
