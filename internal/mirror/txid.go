package mirror

import (
	"fmt"
	"net/url"
	"strings"
)

// TxPath turns an sdk transaction id (0.0.5@1757712000.12345?scheduled/1)
// into the mirror path form (0.0.5-1757712000-000012345) plus query params.
func TxPath(id string) (string, url.Values, error) {
	q := url.Values{}
	// the sdk writes acct@secs.nanos, then ?scheduled, then /nonce
	if base, nonce, ok := strings.Cut(id, "/"); ok {
		q.Set("nonce", nonce)
		id = base
	}
	if base, _, ok := strings.Cut(id, "?"); ok {
		q.Set("scheduled", "true")
		id = base
	}
	acct, ts, ok := strings.Cut(id, "@")
	if !ok {
		return "", nil, fmt.Errorf("transaction id %q has no @", id)
	}
	secs, nanos, ok := strings.Cut(ts, ".")
	if !ok {
		nanos = "0"
	}
	if len(nanos) > 9 {
		return "", nil, fmt.Errorf("transaction id %q has bad nanos", id)
	}
	nanos = strings.Repeat("0", 9-len(nanos)) + nanos
	return acct + "-" + secs + "-" + nanos, q, nil
}
