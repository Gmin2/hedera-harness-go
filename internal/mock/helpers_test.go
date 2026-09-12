package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// harness bundles a running mock with an sdk client pointed at it.
type harness struct {
	t        *testing.T
	srv      *Server
	client   *hiero.Client
	operator hiero.AccountID
	opKey    hiero.PrivateKey
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, Options{})
}

func newHarnessWith(t *testing.T, opts Options) *harness {
	t.Helper()
	srv, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client, err := hiero.ClientForNetworkV2(map[string]hiero.AccountID{srv.ConsensusAddr(): {Account: 3}})
	if err != nil {
		t.Fatal(err)
	}
	client.SetMirrorNetwork([]string{srv.MirrorGRPCAddr()})
	op, key := srv.Operator()
	client.SetOperator(op, key)
	t.Cleanup(func() { client.Close() })

	return &harness{t: t, srv: srv, client: client, operator: op, opKey: key}
}

type actor struct {
	id  hiero.AccountID
	key hiero.PrivateKey
}

func (h *harness) newAccount(hbars float64, maxAuto int32) actor {
	h.t.Helper()
	key, err := hiero.PrivateKeyGenerateEd25519()
	if err != nil {
		h.t.Fatal(err)
	}
	tx := hiero.NewAccountCreateTransaction().
		SetKeyWithoutAlias(key.PublicKey()).
		SetInitialBalance(hiero.NewHbar(hbars)).
		SetMaxAutomaticTokenAssociations(maxAuto)
	receipt := h.run(tx.Execute(h.client))
	return actor{id: *receipt.AccountID, key: key}
}

// run fetches the receipt of a submitted transaction and fails unless it succeeded.
func (h *harness) run(resp hiero.TransactionResponse, err error) hiero.TransactionReceipt {
	h.t.Helper()
	if err != nil {
		h.t.Fatalf("execute: %v", err)
	}
	receipt, err := resp.GetReceipt(h.client)
	if err != nil {
		h.t.Fatalf("receipt: %v", err)
	}
	return receipt
}

// status returns the consensus status without treating failures as errors.
func (h *harness) status(resp hiero.TransactionResponse, err error) hiero.Status {
	h.t.Helper()
	if err != nil {
		h.t.Fatalf("execute: %v", err)
	}
	receipt, err := resp.SetValidateStatus(false).GetReceipt(h.client)
	if err != nil {
		h.t.Fatalf("receipt: %v", err)
	}
	return receipt.Status
}

func expectStatus(t *testing.T, got, want hiero.Status) {
	t.Helper()
	if got != want {
		t.Fatalf("status %s, want %s", got, want)
	}
}

// rest decodes a mirror rest response into out and returns the http status.
func (h *harness) rest(path string, out any) int {
	h.t.Helper()
	resp, err := http.Get(h.srv.MirrorRESTURL() + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func decodeJSON(resp *http.Response, out any) error {
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *harness) mustRest(path string, out any) {
	h.t.Helper()
	if code := h.rest(path, out); code != http.StatusOK {
		h.t.Fatalf("GET %s: http %d", path, code)
	}
}

func mirrorTxPath(id hiero.TransactionID) string {
	return fmt.Sprintf("/transactions/%s-%d-%09d", id.AccountID.String(), id.ValidStart.Unix(), id.ValidStart.Nanosecond())
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
