package mock

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func errNotFound() error { return &httpError{http.StatusNotFound, "Not found"} }

func errInvalidParam(name string) error {
	return &httpError{http.StatusBadRequest, "Invalid parameter: " + name}
}

type page struct {
	limit int
	desc  bool
}

func parsePage(q url.Values, defaultDesc bool) (page, error) {
	p := page{limit: defaultLimit, desc: defaultDesc}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return p, errInvalidParam("limit")
		}
		p.limit = min(n, maxLimit)
	}
	switch strings.ToLower(q.Get("order")) {
	case "":
	case "asc":
		p.desc = false
	case "desc":
		p.desc = true
	default:
		return p, errInvalidParam("order")
	}
	return p, nil
}

// bound is one filter like gt:0.0.5 or a bare value meaning eq.
type bound struct {
	op    string
	value int64
}

type bounds []bound

func (bs bounds) match(v int64) bool {
	for _, b := range bs {
		var pass bool
		switch b.op {
		case "eq":
			pass = v == b.value
		case "ne":
			pass = v != b.value
		case "gt":
			pass = v > b.value
		case "gte":
			pass = v >= b.value
		case "lt":
			pass = v < b.value
		case "lte":
			pass = v <= b.value
		}
		if !pass {
			return false
		}
	}
	return true
}

func parseBounds(q url.Values, name string, parse func(string) (int64, bool)) (bounds, error) {
	var bs bounds
	for _, raw := range q[name] {
		op, value, hasOp := strings.Cut(raw, ":")
		if !hasOp {
			op, value = "eq", raw
		}
		if !slices.Contains([]string{"eq", "ne", "gt", "gte", "lt", "lte"}, op) {
			return nil, errInvalidParam(name)
		}
		v, ok := parse(value)
		if !ok {
			return nil, errInvalidParam(name)
		}
		bs = append(bs, bound{op, v})
	}
	return bs, nil
}

func parseInt(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0
}

// matchKeys filters a row by its sort keys. A links.next cursor over a
// composite key (gte or lte on the leading keys, gt or lt on the last one)
// is compared as a tuple, anything else filters each key on its own.
func matchKeys(values []int64, sets []bounds) bool {
	if cursor, desc, isCursor := asCursor(sets); isCursor {
		c := slices.Compare(values, cursor)
		if desc {
			return c < 0
		}
		return c > 0
	}
	for i, bs := range sets {
		if !bs.match(values[i]) {
			return false
		}
	}
	return true
}

func asCursor(sets []bounds) (cursor []int64, desc bool, ok bool) {
	if len(sets) < 2 {
		return nil, false, false
	}
	for i, bs := range sets {
		if len(bs) != 1 {
			return nil, false, false
		}
		op, last := bs[0].op, i == len(sets)-1
		var rowDesc bool
		switch {
		case last && op == "gt", !last && op == "gte":
		case last && op == "lt", !last && op == "lte":
			rowDesc = true
		default:
			return nil, false, false
		}
		if i > 0 && rowDesc != desc {
			return nil, false, false
		}
		desc = rowDesc
		cursor = append(cursor, bs[0].value)
	}
	return cursor, desc, true
}

// nextLink returns the links.next path for a full page. keys are the sort
// key params and last the values of the final row, rendered as the api does.
func nextLink(r *http.Request, p page, rows int, keys []string, last []string) *string {
	if rows < p.limit {
		return nil
	}
	q := r.URL.Query()
	for i, key := range keys {
		op := "gt"
		if i < len(keys)-1 {
			op = "gte"
		}
		if p.desc {
			op = strings.Replace(op, "gt", "lt", 1)
		}
		// drop earlier cursor bounds going the same way
		kept := q[key][:0]
		for _, v := range q[key] {
			if !strings.HasPrefix(v, op[:2]+":") && !strings.HasPrefix(v, op[:2]+"e:") {
				kept = append(kept, v)
			}
		}
		q[key] = append(kept, op+":"+last[i])
	}
	link := r.URL.Path + "?" + strings.ReplaceAll(q.Encode(), "%3A", ":")
	return &link
}

// parseAccountRef accepts 0.0.5, 5, an evm address with or without 0x, or
// 0.0.<evm address>.
func (st *state) parseAccountRef(s string) (*account, error) {
	evm := strings.TrimPrefix(strings.TrimPrefix(s, "0.0."), "0x")
	if len(evm) == 40 {
		raw, err := hex.DecodeString(evm)
		if err != nil {
			return nil, errInvalidParam("idOrAliasOrEvmAddress")
		}
		if num, found := st.aliases[hex.EncodeToString(raw)]; found {
			return st.accounts[num], nil
		}
		// long zero addresses encode the account number
		if strings.HasPrefix(evm, strings.Repeat("0", 24)) {
			num, _ := strconv.ParseInt(evm[24:], 16, 64)
			if a := st.accounts[num]; a != nil {
				return a, nil
			}
		}
		return nil, errNotFound()
	}
	num, ok := parseEntity(s)
	if !ok {
		return nil, errInvalidParam("idOrAliasOrEvmAddress")
	}
	a := st.accounts[num]
	if a == nil {
		return nil, errNotFound()
	}
	return a, nil
}

func (st *state) parseTokenRef(s string) (*token, error) {
	num, ok := parseEntity(s)
	if !ok {
		return nil, errInvalidParam("tokenId")
	}
	tok := st.tokens[num]
	if tok == nil {
		return nil, errNotFound()
	}
	return tok, nil
}

func pathInt(r *http.Request, name string) (int64, error) {
	n, ok := parseInt(r.PathValue(name))
	if !ok {
		return 0, errInvalidParam(name)
	}
	return n, nil
}
