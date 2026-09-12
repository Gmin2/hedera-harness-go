// Package network builds one sdk client and one mirror client for a mode.
// mock, local and testnet differ only in addresses and operator, so the
// runner and assertions never branch on the mode.
package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/keys"
	"github.com/Gmin2/hedera-harness-go/internal/mirror"
)

type Mode string

const (
	Mock    Mode = "mock"
	Local   Mode = "local"
	Testnet Mode = "testnet"
)

var Modes = []Mode{Mock, Local, Testnet}

func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(strings.TrimSpace(s))); m {
	case Mock, Local, Testnet:
		return m, nil
	case "":
		return Mock, nil
	case "solo", "localnet", "localhost":
		return Local, nil
	}
	return "", fmt.Errorf("unknown network %q (use mock, local or testnet)", s)
}

type Config struct {
	Mode            Mode
	Consensus       string // host:port of node 0.0.3, empty for testnet
	MirrorGRPC      string
	MirrorREST      string // base url ending in /api/v1
	OperatorID      string
	OperatorKey     string
	OperatorKeyType string
	Explorer        string // hashscan style base, empty when there is none
}

// Local profiles. solo is the default, hiero local node is deprecated after sept 2026.
var (
	soloProfile = Config{
		Consensus:  "127.0.0.1:35211",
		MirrorGRPC: "127.0.0.1:5600",
		MirrorREST: "http://127.0.0.1:38081/api/v1",
	}
	localNodeProfile = Config{
		Consensus:  "127.0.0.1:50211",
		MirrorGRPC: "127.0.0.1:5600",
		MirrorREST: "http://127.0.0.1:5551/api/v1",
		Explorer:   "http://localhost:8080/devnet",
	}
)

// genesis operator shared by solo and hiero local node. dev only.
const (
	localOperatorID  = "0.0.2"
	localOperatorKey = "302e020100300506032b65700422042091132178e72057a1d7528025956fe39b0b847f200ab59b2fdd367017f3087137"
)

// FromEnv fills a config for local or testnet from the environment.
// mock configs come from a running mock server instead.
func FromEnv(mode Mode) (Config, error) {
	cfg := Config{Mode: mode}
	switch mode {
	case Local:
		base := soloProfile
		if strings.EqualFold(os.Getenv("HH_LOCAL_PROFILE"), "localnode") {
			base = localNodeProfile
		}
		cfg.Consensus = envOr("HH_CONSENSUS", base.Consensus)
		cfg.MirrorGRPC = envOr("HH_MIRROR_GRPC", base.MirrorGRPC)
		cfg.MirrorREST = envOr("HH_MIRROR_REST", base.MirrorREST)
		cfg.Explorer = envOr("HH_EXPLORER", base.Explorer)
		cfg.OperatorID = firstEnv(localOperatorID, operatorIDVars...)
		cfg.OperatorKey = firstEnv(localOperatorKey, operatorKeyVars...)
	case Testnet:
		cfg.MirrorREST = envOr("HH_MIRROR_REST", "https://testnet.mirrornode.hedera.com/api/v1")
		cfg.Explorer = envOr("HH_EXPLORER", "https://hashscan.io/testnet")
		cfg.OperatorID = firstEnv("", operatorIDVars...)
		cfg.OperatorKey = firstEnv("", operatorKeyVars...)
		if cfg.OperatorID == "" || cfg.OperatorKey == "" {
			return cfg, errors.New("testnet needs an operator: set HEDERA_OPERATOR_ID and HEDERA_OPERATOR_KEY (portal.hedera.com gives you both)")
		}
	default:
		return cfg, fmt.Errorf("FromEnv does not handle %s", mode)
	}
	cfg.OperatorKeyType = firstEnv("", "HH_OPERATOR_KEY_TYPE", "HEDERA_OPERATOR_KEY_TYPE")
	return cfg, nil
}

// the snippets repo alone uses seven names for the same two values
var (
	operatorIDVars  = []string{"HH_OPERATOR_ID", "HEDERA_OPERATOR_ID", "OPERATOR_ID", "HEDERA_ACCOUNT_ID", "ACCOUNT_ID", "MY_ACCOUNT_ID"}
	operatorKeyVars = []string{"HH_OPERATOR_KEY", "HEDERA_OPERATOR_KEY", "OPERATOR_KEY", "HEDERA_PRIVATE_KEY", "PRIVATE_KEY", "MY_PRIVATE_KEY"}
)

type Target struct {
	Mode        Mode
	Client      *hiero.Client
	Mirror      *mirror.Client
	OperatorID  hiero.AccountID
	OperatorKey hiero.PrivateKey
	explorer    string
}

func Connect(ctx context.Context, cfg Config) (*Target, error) {
	opID, err := hiero.AccountIDFromString(cfg.OperatorID)
	if err != nil {
		return nil, fmt.Errorf("operator id %q: %w", cfg.OperatorID, err)
	}
	mc := mirror.New(cfg.MirrorREST)

	opKey, err := keys.Parse(cfg.OperatorKey, cfg.OperatorKeyType)
	if errors.Is(err, keys.ErrAmbiguous) {
		// ask the mirror node which key type the account really has
		opKey, err = resolveKey(ctx, mc, cfg.OperatorID, cfg.OperatorKey)
	}
	if err != nil {
		return nil, fmt.Errorf("operator key: %w", err)
	}

	var client *hiero.Client
	switch cfg.Mode {
	case Testnet:
		client = hiero.ClientForTestnet()
	case Mock, Local:
		client, err = hiero.ClientForNetworkV2(map[string]hiero.AccountID{cfg.Consensus: {Account: 3}})
		if err != nil {
			return nil, err
		}
		client.SetMirrorNetwork([]string{cfg.MirrorGRPC})
		client.SetMinNodeReadmitTime(100 * time.Millisecond)
		client.SetMaxNodeReadmitTime(time.Second)
		client.SetNodeMinBackoff(100 * time.Millisecond)
		client.SetNodeMaxBackoff(time.Second)
		client.SetRequestTimeout(30 * time.Second)
	default:
		return nil, fmt.Errorf("unknown mode %q", cfg.Mode)
	}
	client.SetOperator(opID, opKey)
	// token create costs well over the 2 hbar default
	_ = client.SetDefaultMaxTransactionFee(hiero.NewHbar(50))
	_ = client.SetDefaultMaxQueryPayment(hiero.NewHbar(5))

	return &Target{
		Mode:        cfg.Mode,
		Client:      client,
		Mirror:      mc,
		OperatorID:  opID,
		OperatorKey: opKey,
		explorer:    strings.TrimRight(cfg.Explorer, "/"),
	}, nil
}

func resolveKey(ctx context.Context, mc *mirror.Client, id, raw string) (hiero.PrivateKey, error) {
	acct, _, err := mc.Account(ctx, id)
	if err != nil {
		return hiero.PrivateKey{}, fmt.Errorf("%w (and mirror lookup of %s failed: %v)", keys.ErrAmbiguous, id, err)
	}
	if acct.Key == nil {
		return hiero.PrivateKey{}, keys.ErrAmbiguous
	}
	switch acct.Key.Type {
	case "ED25519":
		return keys.Parse(raw, keys.ED25519)
	case "ECDSA_SECP256K1":
		return keys.Parse(raw, keys.ECDSA)
	}
	return hiero.PrivateKey{}, fmt.Errorf("operator account %s has a %s key, give a der key instead", id, acct.Key.Type)
}

// Link returns an explorer url for a transaction or entity, or "" when the
// network has no explorer (mock).
func (t *Target) Link(kind, id string) string {
	if t.explorer == "" || id == "" {
		return ""
	}
	return t.explorer + "/" + kind + "/" + id
}

func (t *Target) Close() error {
	if t.Client == nil {
		return nil
	}
	return t.Client.Close()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstEnv(fallback string, names ...string) string {
	for _, k := range names {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}
