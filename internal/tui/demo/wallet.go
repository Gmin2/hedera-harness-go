package demo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// Wallet and WalletChoice mirror the tui types, which this package cannot
// import without a cycle in the tui tests.
type Wallet struct {
	Kind    string
	Label   string
	Detail  string
	Account string
	KeyType string
	Balance string
	Status  string
	Note    string
}

type WalletChoice struct {
	Kind      string
	AccountID string
	Key       string
}

const walletDelay = 700 * time.Millisecond

var (
	walletMu sync.Mutex
	// spent grows with every refresh so the demo balance visibly moves
	// after a run.
	spent float64

	accountID = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

var burner = Wallet{
	Kind:    "burner",
	Label:   "Burner wallet",
	Detail:  "fresh per run, funded 20 ℏ",
	KeyType: "ecdsa",
	Status:  "ok",
}

// DefaultWallet is the wallet the demo starts connected to.
func DefaultWallet(network string) Wallet {
	walletMu.Lock()
	defer walletMu.Unlock()
	return defaultWallet(network)
}

func defaultWallet(network string) Wallet {
	switch network {
	case "local":
		return Wallet{
			Kind: "default", Label: "Solo genesis", Detail: "from the solo deployment",
			Account: "0.0.2", KeyType: "ed25519", Status: "error", Note: "local network not reachable",
		}
	case "testnet":
		return Wallet{
			Kind: "default", Label: "Portal account", Detail: "from .env",
			Account: "0.0.6123456", KeyType: "ecdsa", Balance: fmt.Sprintf("%.2f ℏ", max(0, 3.2-spent)),
			Status: "warn", Note: "balance low 3.2 ℏ",
		}
	}
	return Wallet{
		Kind: "default", Label: "Mock operator", Detail: "in process",
		Account: "0.0.2", KeyType: "ed25519", Balance: fmt.Sprintf("%.2f ℏ", 50000-spent), Status: "ok",
	}
}

// Wallets lists the fake wallets for network after a short pause.
func Wallets(ctx context.Context, network string) ([]Wallet, error) {
	if err := pause(ctx); err != nil {
		return nil, err
	}
	walletMu.Lock()
	defer walletMu.Unlock()
	return []Wallet{defaultWallet(network), burner}, nil
}

// ConnectWallet pretends to check a choice against the network.
func ConnectWallet(ctx context.Context, network string, choice WalletChoice) (Wallet, error) {
	if err := pause(ctx); err != nil {
		return Wallet{}, err
	}
	walletMu.Lock()
	defer walletMu.Unlock()

	switch choice.Kind {
	case "burner":
		return burner, nil
	case "import":
		if !accountID.MatchString(choice.AccountID) {
			return Wallet{}, fmt.Errorf("%q is not an account id", choice.AccountID)
		}
		if len(choice.Key) < 32 {
			return Wallet{}, errors.New("key does not match account")
		}
		keyType := "ecdsa"
		if len(choice.Key) >= 4 && choice.Key[:4] == "302e" {
			keyType = "ed25519"
		}
		return Wallet{
			Kind: "import", Label: "Imported key", Detail: "kept in memory",
			Account: choice.AccountID, KeyType: keyType, Balance: fmt.Sprintf("%.2f ℏ", 42-spent), Status: "ok",
		}, nil
	}

	w := defaultWallet(network)
	if w.Status == "error" {
		return Wallet{}, errors.New(w.Note)
	}
	spent += 0.37
	return w, nil
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(walletDelay):
		return nil
	}
}
