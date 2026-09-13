// Package wallet decides which account pays for and signs everything hh
// does on a network: the default operator, a burner funded for one run, or
// an imported key. It is the "connected wallet" of the cli and tui.
package wallet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/keys"
	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/target"
)

const (
	Default = "default"
	Burner  = "burner"
	Import  = "import"
)

// Choice is what the user connected.
type Choice struct {
	Kind      string
	AccountID string  // import
	Key       string  // import
	FundHbar  float64 // burner, 0 picks a default per network
}

func (c Choice) kind() string {
	if c.Kind == "" {
		return Default
	}
	return c.Kind
}

func (c Choice) fund(mode network.Mode) float64 {
	if c.FundHbar > 0 {
		return c.FundHbar
	}
	if mode == network.Testnet {
		// the burner also funds every actor, and token creates cost several hbar
		return 120
	}
	return 100
}

// Info is a checked wallet, ready to show.
type Info struct {
	Kind    string
	Label   string
	Detail  string
	Account string
	KeyType string
	Balance string
	Status  string // ok, warn, error
	Note    string
}

// Open connects to a network with the chosen wallet as operator. A burner is
// created and funded here and swept back to the default operator on close.
func Open(ctx context.Context, mode network.Mode, c Choice) (*network.Target, func(), error) {
	switch c.kind() {
	case Default:
		return target.Open(ctx, mode)
	case Import:
		if mode == network.Mock {
			return nil, nil, errors.New("mock accounts only live for one run, use the default or a burner wallet")
		}
		cfg, _ := network.FromEnv(mode)
		cfg.OperatorID, cfg.OperatorKey, cfg.OperatorKeyType = c.AccountID, c.Key, ""
		t, err := network.Connect(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		return t, func() { t.Close() }, nil
	case Burner:
		return openBurner(ctx, mode, c.fund(mode))
	}
	return nil, nil, fmt.Errorf("unknown wallet %q (use default, burner or import)", c.Kind)
}

func openBurner(ctx context.Context, mode network.Mode, fund float64) (*network.Target, func(), error) {
	t, closeBase, err := target.Open(ctx, mode)
	if err != nil {
		return nil, nil, err
	}
	funder, funderKey := t.OperatorID, t.OperatorKey

	key, err := hiero.PrivateKeyGenerateEcdsa()
	if err != nil {
		closeBase()
		return nil, nil, err
	}
	tx, err := hiero.NewAccountCreateTransaction().
		SetECDSAKeyWithAlias(key.PublicKey()).
		SetInitialBalance(hiero.HbarFromTinybar(int64(fund * 1e8))).
		SetAccountMemo("hh burner").
		FreezeWith(t.Client)
	if err != nil {
		closeBase()
		return nil, nil, err
	}
	resp, err := tx.Sign(key).Execute(t.Client)
	if err == nil {
		var receipt hiero.TransactionReceipt
		receipt, err = resp.GetReceipt(t.Client)
		if err == nil && receipt.AccountID == nil {
			err = errors.New("no account id in the receipt")
		}
		if err == nil {
			t.OperatorID, t.OperatorKey = *receipt.AccountID, key
			t.Client.SetOperator(t.OperatorID, key)
		}
	}
	if err != nil {
		closeBase()
		return nil, nil, fmt.Errorf("fund burner wallet with %g ℏ from %s: %w", fund, funder, err)
	}

	closeFn := func() {
		// mock ledgers vanish with the run, real networks get their hbar back
		if mode != network.Mock {
			sweep(t, funder)
		}
		t.Client.SetOperator(funder, funderKey)
		closeBase()
	}
	return t, closeFn, nil
}

// sweep deletes the burner and sends what is left to the funder.
func sweep(t *network.Target, to hiero.AccountID) {
	tx, err := hiero.NewAccountDeleteTransaction().
		SetAccountID(t.OperatorID).
		SetTransferAccountID(to).
		FreezeWith(t.Client)
	if err != nil {
		return
	}
	if resp, err := tx.Sign(t.OperatorKey).Execute(t.Client); err == nil {
		_, _ = resp.GetReceipt(t.Client)
	}
}

// Env exposes the connected wallet to checks, so deploy scripts and tests
// sign with the same account. The agent itself never gets these.
func Env(t *network.Target) []string {
	env := []string{
		"HH_NETWORK=" + string(t.Mode),
		"HH_OPERATOR_ID=" + t.OperatorID.String(),
		"HH_OPERATOR_KEY=" + t.OperatorKey.StringDer(),
		"HH_MIRROR_REST=" + t.Mirror.Base(),
		"HEDERA_OPERATOR_ID=" + t.OperatorID.String(),
		"HEDERA_OPERATOR_KEY=" + t.OperatorKey.StringDer(),
	}
	switch t.Mode {
	case network.Testnet:
		env = append(env, "HH_JSON_RPC_URL=https://testnet.hashio.io/api")
	case network.Local:
		env = append(env, "HH_JSON_RPC_URL=http://127.0.0.1:7546")
	}
	if keys.Kind(t.OperatorKey) == keys.ECDSA {
		hex := "0x" + t.OperatorKey.StringRaw()
		// scaffold-hbar reads the deployer key from this variable
		env = append(env, "HH_OPERATOR_KEY_HEX="+hex, "__RUNTIME_DEPLOYER_PRIVATE_KEY="+hex)
	}
	return env
}

// List returns the wallets a network offers, checked against the mirror node.
func List(ctx context.Context, mode network.Mode, current Choice) []Info {
	def := checkDefault(ctx, mode)
	burner := Info{
		Kind:   Burner,
		Label:  "Burner wallet",
		Detail: fmt.Sprintf("fresh per run, funded %g ℏ", current.fund(mode)),
		Status: "ok",
	}
	if def.Status == "error" {
		burner.Status, burner.Note = "error", "needs a working default account to fund it: "+def.Note
	}
	out := []Info{def, burner}
	if current.kind() == Import {
		out = append(out, checkImport(ctx, mode, current))
	}
	return out
}

// Connect checks a choice and describes it, or explains why it cannot be used.
func Connect(ctx context.Context, mode network.Mode, c Choice) (Info, error) {
	var info Info
	switch c.kind() {
	case Default:
		info = checkDefault(ctx, mode)
	case Burner:
		for _, w := range List(ctx, mode, c) {
			if w.Kind == Burner {
				info = w
			}
		}
	case Import:
		info = checkImport(ctx, mode, c)
	default:
		return Info{}, fmt.Errorf("unknown wallet %q", c.Kind)
	}
	if info.Status == "error" {
		return info, errors.New(info.Note)
	}
	return info, nil
}

func checkDefault(ctx context.Context, mode network.Mode) Info {
	switch mode {
	case network.Mock:
		return Info{Kind: Default, Label: "Mock operator", Detail: "in process", Account: "0.0.2", KeyType: keys.ED25519, Balance: "1,000,000 ℏ", Status: "ok"}
	case network.Local:
		cfg, _ := network.FromEnv(mode)
		info := checkAccount(ctx, cfg, cfg.OperatorID, cfg.OperatorKey, cfg.OperatorKeyType)
		info.Kind, info.Label, info.Detail = Default, "Solo genesis", strings.TrimPrefix(cfg.Consensus, "127.0.0.1")
		return info
	}
	cfg, err := network.FromEnv(mode)
	info := Info{Kind: Default, Label: "Portal account", Detail: "from .env"}
	if err != nil {
		info.Status, info.Note = "error", "set HEDERA_OPERATOR_ID and HEDERA_OPERATOR_KEY in .env (portal.hedera.com)"
		return info
	}
	checked := checkAccount(ctx, cfg, cfg.OperatorID, cfg.OperatorKey, cfg.OperatorKeyType)
	checked.Kind, checked.Label, checked.Detail = info.Kind, info.Label, info.Detail
	return checked
}

func checkImport(ctx context.Context, mode network.Mode, c Choice) Info {
	info := Info{Kind: Import, Label: "Imported key", Detail: "this session only", Account: c.AccountID}
	if mode == network.Mock {
		info.Status, info.Note = "error", "mock accounts only live for one run, use the default or a burner wallet"
		return info
	}
	cfg, _ := network.FromEnv(mode)
	checked := checkAccount(ctx, cfg, c.AccountID, c.Key, "")
	checked.Kind, checked.Label, checked.Detail = info.Kind, info.Label, info.Detail
	return checked
}

// checkAccount is the wallet side of hh doctor: the key parses, the account
// exists, the key matches it, and there is enough hbar to do something.
func checkAccount(ctx context.Context, cfg network.Config, id, rawKey, keyType string) Info {
	info := Info{Account: id}
	if _, err := hiero.AccountIDFromString(id); err != nil {
		info.Status, info.Note = "error", fmt.Sprintf("%q is not an account id", id)
		return info
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	mc := mirror.New(cfg.MirrorREST)
	acct, _, err := mc.Account(ctx, id)
	if err != nil {
		info.Status = "error"
		if errors.Is(err, mirror.ErrNotFound) {
			info.Note = "account not found on the mirror node"
		} else {
			info.Note = cfg.MirrorREST + " not reachable"
		}
		return info
	}
	info.Balance = formatHbar(acct.Balance.Balance)

	key, err := keys.Parse(rawKey, keyType)
	if errors.Is(err, keys.ErrAmbiguous) && acct.Key != nil {
		kind := keys.ECDSA
		if acct.Key.Type == "ED25519" {
			kind = keys.ED25519
		}
		key, err = keys.Parse(rawKey, kind)
	}
	if err != nil {
		info.Status, info.Note = "error", "key: "+err.Error()
		return info
	}
	info.KeyType = keys.Kind(key)
	if acct.Key != nil && !strings.HasSuffix(strings.ToLower(acct.Key.Key), strings.ToLower(key.PublicKey().StringRaw())) {
		info.Status, info.Note = "error", "key does not match the account, every transaction would fail"
		return info
	}
	info.Status = "ok"
	if acct.Balance.Balance < 5*100_000_000 {
		info.Status, info.Note = "warn", "balance low, top up at portal.hedera.com/faucet"
	}
	return info
}

func formatHbar(tinybars int64) string {
	h := float64(tinybars) / 1e8
	switch {
	case h >= 1000:
		return fmt.Sprintf("%s ℏ", thousands(int64(h)))
	case h >= 1:
		return fmt.Sprintf("%.2f ℏ", h)
	}
	return fmt.Sprintf("%.4f ℏ", h)
}

func thousands(n int64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
