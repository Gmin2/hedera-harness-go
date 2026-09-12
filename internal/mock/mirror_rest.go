package mock

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
)

// restView is what one rest request may see: the state as of the mirror lag
// and the transactions that reached consensus up to then.
type restView struct {
	st     *state
	at     time.Time
	ledger *ledger
}

type restHandler func(v *restView, r *http.Request) (any, error)

func newMirrorREST(l *ledger) http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, fn restHandler) {
		mux.HandleFunc("GET /api/v1"+pattern, func(w http.ResponseWriter, r *http.Request) {
			body, err := func() (any, error) {
				l.mu.Lock()
				defer l.mu.Unlock()
				st, at := l.view()
				return fn(&restView{st: st, at: at, ledger: l}, r)
			}()
			writeJSON(w, body, err)
		})
	}

	route("/accounts/{id}", getAccount)
	route("/accounts/{id}/tokens", getAccountTokens)
	route("/accounts/{id}/nfts", getAccountNfts)
	route("/accounts/{id}/airdrops/pending", getAirdrops(true))
	route("/accounts/{id}/airdrops/outstanding", getAirdrops(false))
	route("/balances", getBalances)
	route("/tokens/{id}", getToken)
	route("/tokens/{id}/balances", getTokenBalances)
	route("/tokens/{id}/nfts", getTokenNfts)
	route("/tokens/{id}/nfts/{serial}", getTokenNft)
	route("/topics/{id}", getTopic)
	route("/topics/{id}/messages", getTopicMessages)
	route("/topics/{id}/messages/{seq}", getTopicMessage)
	route("/schedules/{id}", getSchedule)
	route("/transactions/{id}", getTransaction)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, nil, errNotFound())
	})
	return mux
}

func writeJSON(w http.ResponseWriter, body any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status, message := http.StatusInternalServerError, err.Error()
		var he *httpError
		if errors.As(err, &he) {
			status = he.status
		}
		w.WriteHeader(status)
		body = mirror.Error{Status: mirror.ErrorStatus{Messages: []mirror.ErrorMessage{{Message: message}}}}
	}
	json.NewEncoder(w).Encode(body)
}

// paginate walks rows, which must be sorted ascending, in page order and
// keeps the ones that pass keep up to the page limit.
func paginate[T any](rows []T, p page, keep func(T) bool) []T {
	if p.desc {
		slices.Reverse(rows)
	}
	out := []T{}
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
			if len(out) == p.limit {
				break
			}
		}
	}
	return out
}

func getAccount(v *restView, r *http.Request) (any, error) {
	a, err := v.st.parseAccountRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return renderAccount(a, mirrorTime(v.at)), nil
}

func getAccountTokens(v *restView, r *http.Request) (any, error) {
	a, err := v.st.parseAccountRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	p, err := parsePage(q, false)
	if err != nil {
		return nil, err
	}
	tokenIDs, err := parseBounds(q, "token.id", parseEntity)
	if err != nil {
		return nil, err
	}

	nums := paginate(slices.Sorted(maps.Keys(a.tokens)), p, tokenIDs.match)
	out := mirror.TokenRelationships{Tokens: []mirror.TokenRelationship{}}
	for _, num := range nums {
		out.Tokens = append(out.Tokens, renderRelationship(v.st.tokens[num], a.tokens[num]))
	}
	if len(nums) > 0 {
		out.Links.Next = nextLink(r, p, len(nums), []string{"token.id"}, []string{entityString(nums[len(nums)-1])})
	}
	return out, nil
}

type ownedNft struct {
	tok *token
	nft *nft
}

func getAccountNfts(v *restView, r *http.Request) (any, error) {
	a, err := v.st.parseAccountRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	p, err := parsePage(q, true)
	if err != nil {
		return nil, err
	}
	tokenIDs, err := parseBounds(q, "token.id", parseEntity)
	if err != nil {
		return nil, err
	}
	serials, err := parseBounds(q, "serialnumber", parseInt)
	if err != nil {
		return nil, err
	}

	var rows []ownedNft
	for _, num := range slices.Sorted(maps.Keys(a.tokens)) {
		tok := v.st.tokens[num]
		for _, serial := range slices.Sorted(maps.Keys(tok.nfts)) {
			if n := tok.nfts[serial]; n.owner == a.num {
				rows = append(rows, ownedNft{tok, n})
			}
		}
	}
	rows = paginate(rows, p, func(o ownedNft) bool {
		return matchKeys([]int64{o.tok.num, o.nft.serial}, []bounds{tokenIDs, serials})
	})

	out := mirror.Nfts{Nfts: []mirror.Nft{}}
	for _, o := range rows {
		out.Nfts = append(out.Nfts, renderNft(o.tok, o.nft))
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		out.Links.Next = nextLink(r, p, len(rows), []string{"token.id", "serialnumber"},
			[]string{entityString(last.tok.num), strconv.FormatInt(last.nft.serial, 10)})
	}
	return out, nil
}

// getAirdrops lists airdrops the account received (pending) or sent (outstanding).
func getAirdrops(received bool) restHandler {
	return func(v *restView, r *http.Request) (any, error) {
		a, err := v.st.parseAccountRef(r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		p, err := parsePage(q, false)
		if err != nil {
			return nil, err
		}
		otherParam := "receiver.id"
		if received {
			otherParam = "sender.id"
		}
		others, err := parseBounds(q, otherParam, parseEntity)
		if err != nil {
			return nil, err
		}
		tokenIDs, err := parseBounds(q, "token.id", parseEntity)
		if err != nil {
			return nil, err
		}
		serials, err := parseBounds(q, "serialnumber", parseInt)
		if err != nil {
			return nil, err
		}

		other := func(p *pendingAirdrop) int64 {
			if received {
				return p.sender
			}
			return p.receiver
		}
		var rows []*pendingAirdrop
		for _, p := range v.st.airdrops {
			if (received && p.receiver == a.num) || (!received && p.sender == a.num) {
				rows = append(rows, p)
			}
		}
		slices.SortFunc(rows, func(x, y *pendingAirdrop) int {
			return cmp.Or(cmp.Compare(other(x), other(y)), cmp.Compare(x.token, y.token), cmp.Compare(x.serial, y.serial))
		})
		rows = paginate(rows, p, func(row *pendingAirdrop) bool {
			return matchKeys([]int64{other(row), row.token, row.serial}, []bounds{others, tokenIDs, serials})
		})

		out := mirror.TokenAirdrops{Airdrops: []mirror.TokenAirdrop{}}
		for _, row := range rows {
			out.Airdrops = append(out.Airdrops, renderAirdrop(row))
		}
		if len(rows) > 0 {
			last := rows[len(rows)-1]
			out.Links.Next = nextLink(r, p, len(rows), []string{otherParam, "token.id", "serialnumber"},
				[]string{entityString(other(last)), entityString(last.token), strconv.FormatInt(last.serial, 10)})
		}
		return out, nil
	}
}

func getBalances(v *restView, r *http.Request) (any, error) {
	q := r.URL.Query()
	p, err := parsePage(q, true)
	if err != nil {
		return nil, err
	}
	ids, err := parseBounds(q, "account.id", parseEntity)
	if err != nil {
		return nil, err
	}
	nums := paginate(slices.Sorted(maps.Keys(v.st.accounts)), p, ids.match)
	out := balancesResponse{Timestamp: mirrorTime(v.at), Balances: []accountBalanceRow{}}
	for _, num := range nums {
		a := v.st.accounts[num]
		out.Balances = append(out.Balances, accountBalanceRow{
			Account: entityString(num),
			Balance: a.balance,
			Tokens:  renderTokenBalances(a),
		})
	}
	if len(nums) > 0 {
		out.Links.Next = nextLink(r, p, len(nums), []string{"account.id"}, []string{entityString(nums[len(nums)-1])})
	}
	return out, nil
}

func getToken(v *restView, r *http.Request) (any, error) {
	tok, err := v.st.parseTokenRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return renderToken(tok), nil
}

func getTokenBalances(v *restView, r *http.Request) (any, error) {
	tok, err := v.st.parseTokenRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	p, err := parsePage(q, true)
	if err != nil {
		return nil, err
	}
	ids, err := parseBounds(q, "account.id", parseEntity)
	if err != nil {
		return nil, err
	}

	var holders []int64
	for _, num := range slices.Sorted(maps.Keys(v.st.accounts)) {
		if v.st.accounts[num].tokens[tok.num] != nil {
			holders = append(holders, num)
		}
	}
	holders = paginate(holders, p, ids.match)
	out := tokenBalancesResponse{Timestamp: mirrorTime(v.at), Balances: []tokenBalanceRow{}}
	for _, num := range holders {
		out.Balances = append(out.Balances, tokenBalanceRow{
			Account:  entityString(num),
			Balance:  v.st.accounts[num].tokens[tok.num].balance,
			Decimals: int64(tok.spec.Decimals),
		})
	}
	if len(holders) > 0 {
		out.Links.Next = nextLink(r, p, len(holders), []string{"account.id"}, []string{entityString(holders[len(holders)-1])})
	}
	return out, nil
}

func getTokenNfts(v *restView, r *http.Request) (any, error) {
	tok, err := v.st.parseTokenRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	p, err := parsePage(q, true)
	if err != nil {
		return nil, err
	}
	serials, err := parseBounds(q, "serialnumber", parseInt)
	if err != nil {
		return nil, err
	}
	owners, err := parseBounds(q, "account.id", parseEntity)
	if err != nil {
		return nil, err
	}

	keep := paginate(slices.Sorted(maps.Keys(tok.nfts)), p, func(serial int64) bool {
		return serials.match(serial) && owners.match(tok.nfts[serial].owner)
	})
	out := mirror.Nfts{Nfts: []mirror.Nft{}}
	for _, serial := range keep {
		out.Nfts = append(out.Nfts, renderNft(tok, tok.nfts[serial]))
	}
	if len(keep) > 0 {
		out.Links.Next = nextLink(r, p, len(keep), []string{"serialnumber"}, []string{strconv.FormatInt(keep[len(keep)-1], 10)})
	}
	return out, nil
}

func getTokenNft(v *restView, r *http.Request) (any, error) {
	tok, err := v.st.parseTokenRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	serial, err := pathInt(r, "serial")
	if err != nil {
		return nil, err
	}
	n := tok.nfts[serial]
	if n == nil {
		return nil, errNotFound()
	}
	return renderNft(tok, n), nil
}

func (v *restView) topic(r *http.Request) (*topic, error) {
	num, ok := parseEntity(r.PathValue("id"))
	if !ok {
		return nil, errInvalidParam("topicId")
	}
	tp := v.st.topics[num]
	if tp == nil {
		return nil, errNotFound()
	}
	return tp, nil
}

func getTopic(v *restView, r *http.Request) (any, error) {
	tp, err := v.topic(r)
	if err != nil {
		return nil, err
	}
	return renderTopic(tp), nil
}

func getTopicMessages(v *restView, r *http.Request) (any, error) {
	tp, err := v.topic(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	p, err := parsePage(q, false)
	if err != nil {
		return nil, err
	}
	seqs, err := parseBounds(q, "sequencenumber", parseInt)
	if err != nil {
		return nil, err
	}

	msgs := paginate(slices.Clone(tp.messages), p, func(m *topicMessage) bool {
		return seqs.match(int64(m.sequence))
	})
	out := mirror.TopicMessages{Messages: []mirror.TopicMessage{}}
	for _, m := range msgs {
		out.Messages = append(out.Messages, renderTopicMessage(m))
	}
	if len(msgs) > 0 {
		out.Links.Next = nextLink(r, p, len(msgs), []string{"sequencenumber"}, []string{strconv.FormatUint(msgs[len(msgs)-1].sequence, 10)})
	}
	return out, nil
}

func getTopicMessage(v *restView, r *http.Request) (any, error) {
	tp, err := v.topic(r)
	if err != nil {
		return nil, err
	}
	seq, err := pathInt(r, "seq")
	if err != nil {
		return nil, err
	}
	if seq < 1 || seq > int64(len(tp.messages)) {
		return nil, errNotFound()
	}
	return renderTopicMessage(tp.messages[seq-1]), nil
}

func getSchedule(v *restView, r *http.Request) (any, error) {
	num, ok := parseEntity(r.PathValue("id"))
	if !ok {
		return nil, errInvalidParam("scheduleId")
	}
	sc := v.st.schedules[num]
	if sc == nil {
		return nil, errNotFound()
	}
	return renderSchedule(sc), nil
}

func getTransaction(v *restView, r *http.Request) (any, error) {
	want, ok := parseMirrorTxID(r.PathValue("id"))
	if !ok {
		return nil, &httpError{http.StatusBadRequest, "Invalid Transaction id. Please use shard.realm.num-sss-nnn format where sss are seconds and nnn are nanoseconds"}
	}
	q := r.URL.Query()
	nonce := int64(-1)
	if s := q.Get("nonce"); s != "" {
		n, ok := parseInt(s)
		if !ok || n > 1<<31-1 {
			return nil, errInvalidParam("nonce")
		}
		nonce = n
	}
	var scheduled *bool
	if s := q.Get("scheduled"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, errInvalidParam("scheduled")
		}
		scheduled = &b
	}

	out := mirror.Transactions{Transactions: []mirror.Transaction{}}
	for _, rec := range v.ledger.history {
		if rec.consensus.After(v.at) {
			break
		}
		id := rec.id
		switch {
		case id.payer != want.payer || id.seconds != want.seconds || id.nanos != want.nanos:
			continue
		case nonce >= 0 && int64(id.nonce) != nonce:
			continue
		case scheduled != nil && id.scheduled != *scheduled:
			continue
		}
		out.Transactions = append(out.Transactions, renderTransaction(rec))
	}
	if len(out.Transactions) == 0 {
		return nil, errNotFound()
	}
	return out, nil
}

// evmAddress is the alias derived address when there is one, otherwise the
// long zero form of the account number.
func evmAddress(a *account) string {
	if len(a.alias) == 20 {
		return "0x" + hex.EncodeToString(a.alias)
	}
	return fmt.Sprintf("0x%040x", a.num)
}
