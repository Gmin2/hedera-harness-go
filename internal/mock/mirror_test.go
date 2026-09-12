package mock

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestMirrorLag(t *testing.T) {
	h := newHarnessWith(t, Options{MirrorLag: 300 * time.Millisecond})
	alice := h.newAccount(1, 0)

	if code := h.rest("/accounts/"+alice.id.String(), nil); code != http.StatusNotFound {
		t.Fatalf("account visible before the lag passed: http %d", code)
	}
	waitFor(t, "account on the lagging mirror", func() bool {
		return h.rest("/accounts/"+alice.id.String(), nil) == http.StatusOK
	})
}

func TestMirrorPaginationAndErrors(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	var tokens []hiero.TokenID
	for range 3 {
		tokens = append(tokens, h.fungibleToken(treasury, nil))
	}

	var first mirror.TokenRelationships
	h.mustRest("/accounts/"+treasury.id.String()+"/tokens?limit=2", &first)
	if len(first.Tokens) != 2 || first.Links.Next == nil {
		t.Fatalf("first page %+v", first)
	}
	next := *first.Links.Next
	if !strings.HasPrefix(next, "/api/v1/accounts/") || !strings.Contains(next, "token.id=gt:"+tokens[1].String()) {
		t.Fatalf("links.next %q", next)
	}
	var second mirror.TokenRelationships
	h.mustRest(strings.TrimPrefix(next, "/api/v1"), &second)
	if len(second.Tokens) != 1 || second.Tokens[0].TokenID != tokens[2].String() || second.Links.Next != nil {
		t.Fatalf("second page %+v", second)
	}

	resp, err := http.Get(h.srv.MirrorRESTURL() + "/tokens/0.0.424242")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body mirror.Error
	if resp.StatusCode != http.StatusNotFound || decodeJSON(resp, &body) != nil || body.Status.Messages[0].Message != "Not found" {
		t.Fatalf("missing token: http %d body %+v", resp.StatusCode, body)
	}

	for _, path := range []string{
		"/transactions/not-a-tx-id",
		"/accounts/" + treasury.id.String() + "/tokens?limit=zero",
		"/accounts/" + treasury.id.String() + "/tokens?order=sideways",
		"/topics/abc/messages",
	} {
		if code := h.rest(path, nil); code != http.StatusBadRequest {
			t.Errorf("GET %s: http %d, want 400", path, code)
		}
	}
}

// Every query type the sdk can send must get a response variant back, even
// the unsupported ones, or the sdk dereferences a nil header.
func TestUnsupportedQueriesKeepTheirVariant(t *testing.T) {
	oneof := (&services.Query{}).ProtoReflect().Descriptor().Oneofs().ByName("query")
	fields := oneof.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		q := &services.Query{}
		qr := q.ProtoReflect()
		qr.Set(field, protoreflect.ValueOfMessage(qr.NewField(field).Message()))

		resp := unsupportedQuery(q)
		if resp.Response == nil {
			t.Errorf("%s: no response variant", field.Name())
		}
	}
}

func TestFixedClockStillOrdersConsensus(t *testing.T) {
	fixed := time.Now()
	h := newHarnessWith(t, Options{Now: func() time.Time { return fixed }})
	a := h.newAccount(1, 0)
	b := h.newAccount(1, 0)

	var first, second mirror.Account
	h.mustRest("/accounts/"+a.id.String(), &first)
	h.mustRest("/accounts/"+b.id.String(), &second)
	if first.CreatedTimestamp >= second.CreatedTimestamp {
		t.Fatalf("created timestamps not increasing: %s then %s", first.CreatedTimestamp, second.CreatedTimestamp)
	}
}
