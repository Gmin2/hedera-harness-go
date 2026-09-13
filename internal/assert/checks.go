package assert

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
)

type accountHbar struct {
	Account string `yaml:"account"`
	Num     `yaml:",inline"`
}

func (c *accountHbar) Title() string    { return c.Account + " hbar" }
func (c *accountHbar) Expected() string { return c.Num.String(" ℏ") }
func (c *accountHbar) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Account, "account"); err != nil {
		return err
	}
	return requireNum(c.Num, "account.hbar")
}

func (c *accountHbar) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	id, _ := env.Lookup(c.Account)
	a, src, err := m.Account(ctx, id)
	if err != nil {
		return Observation{Source: src}, err
	}
	return Observation{Actual: tinybarsToHbar(a.Balance.Balance), OK: c.match(a.Balance.Balance, 1e8), Source: src}, nil
}

type tokenBalance struct {
	Account string `yaml:"account"`
	Token   string `yaml:"token"`
	Num     `yaml:",inline"`
}

func (c *tokenBalance) Title() string    { return c.Account + " " + c.Token + " balance" }
func (c *tokenBalance) Expected() string { return c.Num.String("") }
func (c *tokenBalance) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Account, "account"); err != nil {
		return err
	}
	if _, err := lookup(env, c.Token, "token"); err != nil {
		return err
	}
	return requireNum(c.Num, "token.balance")
}

func (c *tokenBalance) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	acct, _ := env.Lookup(c.Account)
	tok, _ := env.Lookup(c.Token)
	rels, src, err := m.AccountTokens(ctx, acct, tok)
	if err != nil {
		return Observation{Source: src}, err
	}
	for _, r := range rels {
		if r.TokenID == tok {
			return Observation{Actual: strconv.FormatInt(r.Balance, 10), OK: c.match(r.Balance, 1), Source: src}, nil
		}
	}
	// no relationship means the account holds none of it
	return Observation{Actual: "0 (not associated)", OK: c.match(0, 1), Source: src}, nil
}

type tokenSupply struct {
	Token string `yaml:"token"`
	Num   `yaml:",inline"`
}

func (c *tokenSupply) Title() string    { return c.Token + " total supply" }
func (c *tokenSupply) Expected() string { return c.Num.String("") }
func (c *tokenSupply) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Token, "token"); err != nil {
		return err
	}
	return requireNum(c.Num, "token.supply")
}

func (c *tokenSupply) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	tok, _ := env.Lookup(c.Token)
	t, src, err := m.Token(ctx, tok)
	if err != nil {
		return Observation{Source: src}, err
	}
	supply, err := strconv.ParseInt(t.TotalSupply, 10, 64)
	if err != nil {
		return Observation{Source: src}, fmt.Errorf("total_supply %q: %w", t.TotalSupply, err)
	}
	return Observation{Actual: t.TotalSupply, OK: c.match(supply, 1), Source: src}, nil
}

type tokenRelationship struct {
	Account    string `yaml:"account"`
	Token      string `yaml:"token"`
	Associated *bool  `yaml:"associated"`
	Kyc        string `yaml:"kyc"`    // granted, revoked, not_applicable
	Freeze     string `yaml:"freeze"` // frozen, unfrozen, not_applicable
	Automatic  *bool  `yaml:"automatic"`
}

func (c *tokenRelationship) Title() string { return c.Account + " ↔ " + c.Token }

func (c *tokenRelationship) Expected() string {
	var p []string
	if c.Associated != nil {
		p = append(p, "associated="+strconv.FormatBool(*c.Associated))
	}
	if c.Kyc != "" {
		p = append(p, "kyc="+strings.ToLower(c.Kyc))
	}
	if c.Freeze != "" {
		p = append(p, "freeze="+strings.ToLower(c.Freeze))
	}
	if c.Automatic != nil {
		p = append(p, "automatic="+strconv.FormatBool(*c.Automatic))
	}
	return strings.Join(p, " ")
}

func (c *tokenRelationship) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Account, "account"); err != nil {
		return err
	}
	if _, err := lookup(env, c.Token, "token"); err != nil {
		return err
	}
	if c.Associated == nil && c.Kyc == "" && c.Freeze == "" && c.Automatic == nil {
		return fmt.Errorf("token.relationship needs associated, kyc, freeze or automatic")
	}
	return nil
}

func (c *tokenRelationship) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	acct, _ := env.Lookup(c.Account)
	tok, _ := env.Lookup(c.Token)
	rels, src, err := m.AccountTokens(ctx, acct, tok)
	if err != nil {
		return Observation{Source: src}, err
	}
	for _, r := range rels {
		if r.TokenID != tok {
			continue
		}
		actual := fmt.Sprintf("associated=true kyc=%s freeze=%s automatic=%t",
			strings.ToLower(r.KycStatus), strings.ToLower(r.FreezeStatus), r.AutomaticAssociation)
		ok := c.Associated == nil || *c.Associated
		ok = ok && (c.Kyc == "" || strings.EqualFold(c.Kyc, r.KycStatus))
		ok = ok && (c.Freeze == "" || strings.EqualFold(c.Freeze, r.FreezeStatus))
		ok = ok && (c.Automatic == nil || *c.Automatic == r.AutomaticAssociation)
		return Observation{Actual: actual, OK: ok, Source: src}, nil
	}
	ok := c.Associated != nil && !*c.Associated
	return Observation{Actual: "associated=false", OK: ok, Source: src}, nil
}

type tokenPaused struct {
	Token  string `yaml:"token"`
	Equals *bool  `yaml:"equals"`
}

func (c *tokenPaused) want() bool       { return c.Equals == nil || *c.Equals }
func (c *tokenPaused) Title() string    { return c.Token + " paused" }
func (c *tokenPaused) Expected() string { return strconv.FormatBool(c.want()) }
func (c *tokenPaused) Resolve(env *ops.Env) error {
	_, err := lookup(env, c.Token, "token")
	return err
}

func (c *tokenPaused) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	tok, _ := env.Lookup(c.Token)
	t, src, err := m.Token(ctx, tok)
	if err != nil {
		return Observation{Source: src}, err
	}
	paused := t.PauseStatus == "PAUSED"
	return Observation{Actual: strconv.FormatBool(paused) + " (" + strings.ToLower(t.PauseStatus) + ")", OK: paused == c.want(), Source: src}, nil
}

type nftOwner struct {
	Token  string `yaml:"token"`
	Serial int64  `yaml:"serial"`
	Owner  string `yaml:"owner"`
}

func (c *nftOwner) Title() string    { return fmt.Sprintf("%s #%d owner", c.Token, c.Serial) }
func (c *nftOwner) Expected() string { return c.Owner }
func (c *nftOwner) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Token, "token"); err != nil {
		return err
	}
	if c.Serial <= 0 {
		return fmt.Errorf("nft.owner needs serial")
	}
	_, err := lookup(env, c.Owner, "owner")
	return err
}

func (c *nftOwner) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	tok, _ := env.Lookup(c.Token)
	want, _ := env.Lookup(c.Owner)
	n, src, err := m.Nft(ctx, tok, c.Serial)
	if err != nil {
		return Observation{Source: src}, err
	}
	actual := n.AccountID
	if name := env.NameOf(n.AccountID); name != "" {
		actual = name + " (" + n.AccountID + ")"
	}
	return Observation{Actual: actual, OK: n.AccountID == want, Source: src}, nil
}

type topicMessages struct {
	Topic    string `yaml:"topic"`
	Count    *int   `yaml:"count"`
	Contains string `yaml:"contains"`
	Sequence int64  `yaml:"sequence"`
	Equals   string `yaml:"equals"` // message text at sequence
	// VerifyChain recomputes the v3 running hash of every message, proving
	// none were changed, dropped or reordered.
	VerifyChain bool `yaml:"verify_chain"`
}

func (c *topicMessages) Title() string { return c.Topic + " messages" }

func (c *topicMessages) Expected() string {
	var p []string
	if c.Count != nil {
		p = append(p, fmt.Sprintf("count = %d", *c.Count))
	}
	if c.Contains != "" {
		p = append(p, fmt.Sprintf("a message contains %q", c.Contains))
	}
	if c.Sequence > 0 {
		p = append(p, fmt.Sprintf("#%d = %q", c.Sequence, c.Equals))
	}
	if c.VerifyChain {
		p = append(p, "running hash chain intact")
	}
	return strings.Join(p, " and ")
}

func (c *topicMessages) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Topic, "topic"); err != nil {
		return err
	}
	if c.Count == nil && c.Contains == "" && c.Sequence == 0 && !c.VerifyChain {
		return fmt.Errorf("topic.messages needs count, contains, sequence with equals, or verify_chain")
	}
	return nil
}

func (c *topicMessages) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	id, _ := env.Lookup(c.Topic)
	msgs, src, err := m.TopicMessages(ctx, id)
	if err != nil {
		return Observation{Source: src}, err
	}
	var texts []string
	for _, msg := range msgs {
		b, err := base64.StdEncoding.DecodeString(msg.Message)
		if err != nil {
			b = []byte(msg.Message)
		}
		texts = append(texts, string(b))
	}
	ok := true
	var actual []string
	if c.Count != nil {
		ok = ok && len(msgs) == *c.Count
		actual = append(actual, fmt.Sprintf("count = %d", len(msgs)))
	}
	if c.Contains != "" {
		found := false
		for _, t := range texts {
			if strings.Contains(t, c.Contains) {
				found = true
				break
			}
		}
		ok = ok && found
		if !found {
			actual = append(actual, fmt.Sprintf("no message contains %q", c.Contains))
		}
	}
	if c.Sequence > 0 {
		got := "(missing)"
		for i, msg := range msgs {
			if msg.SequenceNumber == c.Sequence {
				got = strconv.Quote(texts[i])
			}
		}
		ok = ok && got == strconv.Quote(c.Equals)
		actual = append(actual, fmt.Sprintf("#%d = %s", c.Sequence, got))
	}
	if c.VerifyChain {
		if at, err := mirror.VerifyChain(id, msgs); err != nil {
			ok = false
			actual = append(actual, fmt.Sprintf("chain broken at #%d: %v", at, err))
		} else {
			actual = append(actual, fmt.Sprintf("chain intact across %d messages", len(msgs)))
		}
	}
	return Observation{Actual: strings.Join(actual, " and "), OK: ok, Source: src}, nil
}

type scheduleExecuted struct {
	Schedule string `yaml:"schedule"`
	Equals   *bool  `yaml:"equals"`
	// Result is the status the inner transaction must end with once executed.
	// A schedule counts as executed even when its transaction failed, so
	// without this a reverted payout would still pass.
	Result string `yaml:"result"`
}

func (c *scheduleExecuted) want() bool { return c.Equals == nil || *c.Equals }
func (c *scheduleExecuted) wantResult() string {
	if c.Result == "" {
		return "SUCCESS"
	}
	return strings.ToUpper(c.Result)
}
func (c *scheduleExecuted) Title() string { return c.Schedule + " executed" }
func (c *scheduleExecuted) Expected() string {
	if !c.want() {
		return "false"
	}
	return "true, inner tx " + c.wantResult()
}
func (c *scheduleExecuted) Resolve(env *ops.Env) error {
	_, err := lookup(env, c.Schedule, "schedule")
	return err
}

func (c *scheduleExecuted) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	id, _ := env.Lookup(c.Schedule)
	s, src, err := m.Schedule(ctx, id)
	if err != nil {
		return Observation{Source: src}, err
	}
	executed := s.ExecutedTimestamp != nil
	if !executed {
		actual := fmt.Sprintf("false (%d signatures)", len(s.Signatures))
		return Observation{Actual: actual, OK: !c.want(), Source: src}, nil
	}
	if !c.want() {
		return Observation{Actual: "true at " + *s.ExecutedTimestamp, OK: false, Source: src}, nil
	}

	txID, ok := env.ScheduledTx(c.Schedule)
	if !ok {
		// a literal schedule id from outside the scenario, only the timestamp is known
		return Observation{Actual: "true at " + *s.ExecutedTimestamp + " (inner result unknown)", OK: true, Source: src}, nil
	}
	tx, txSrc, err := m.Transaction(ctx, txID)
	if err != nil {
		return Observation{Source: txSrc}, fmt.Errorf("executed, but the inner transaction %s is not on the mirror yet: %w", txID, err)
	}
	actual := "true, inner tx " + tx.Result
	return Observation{Actual: actual, OK: strings.EqualFold(tx.Result, c.wantResult()), Source: txSrc}, nil
}

type airdropPending struct {
	Receiver string `yaml:"receiver"`
	Sender   string `yaml:"sender"`
	Token    string `yaml:"token"`
	Exists   *bool  `yaml:"exists"`
	Amount   *int64 `yaml:"amount"`
}

func (c *airdropPending) want() bool    { return c.Exists == nil || *c.Exists }
func (c *airdropPending) Title() string { return "pending airdrop " + c.Token + " → " + c.Receiver }
func (c *airdropPending) Expected() string {
	if !c.want() {
		return "none"
	}
	if c.Amount != nil {
		return fmt.Sprintf("pending amount %d", *c.Amount)
	}
	return "pending"
}

func (c *airdropPending) Resolve(env *ops.Env) error {
	if _, err := lookup(env, c.Receiver, "receiver"); err != nil {
		return err
	}
	if c.Sender != "" {
		if _, err := lookup(env, c.Sender, "sender"); err != nil {
			return err
		}
	}
	_, err := lookup(env, c.Token, "token")
	return err
}

func (c *airdropPending) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	recv, _ := env.Lookup(c.Receiver)
	tok, _ := env.Lookup(c.Token)
	sender := ""
	if c.Sender != "" {
		sender, _ = env.Lookup(c.Sender)
	}
	list, src, err := m.PendingAirdrops(ctx, recv, tok)
	if err != nil {
		return Observation{Source: src}, err
	}
	var total int64
	found := false
	for _, a := range list {
		if a.TokenID != tok || (sender != "" && a.SenderID != sender) {
			continue
		}
		found = true
		total += a.Amount
	}
	if !found {
		return Observation{Actual: "none", OK: !c.want(), Source: src}, nil
	}
	ok := c.want() && (c.Amount == nil || *c.Amount == total)
	return Observation{Actual: fmt.Sprintf("pending amount %d", total), OK: ok, Source: src}, nil
}

func init() {
	register("account.hbar", func() Check { return &accountHbar{} })
	register("token.balance", func() Check { return &tokenBalance{} })
	register("token.supply", func() Check { return &tokenSupply{} })
	register("token.relationship", func() Check { return &tokenRelationship{} })
	register("token.paused", func() Check { return &tokenPaused{} })
	register("nft.owner", func() Check { return &nftOwner{} })
	register("topic.messages", func() Check { return &topicMessages{} })
	register("schedule.executed", func() Check { return &scheduleExecuted{} })
	register("airdrop.pending", func() Check { return &airdropPending{} })
}
