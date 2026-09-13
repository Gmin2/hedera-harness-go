package cli

import (
	"context"
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/tui"
	"github.com/Gmin2/hedera-harness-go/internal/wallet"
)

// the connected wallet, shared by every command and the tui
var (
	walletMu  sync.Mutex
	connected wallet.Choice
)

func currentWallet() wallet.Choice {
	walletMu.Lock()
	defer walletMu.Unlock()
	return connected
}

func setWallet(c wallet.Choice) {
	walletMu.Lock()
	connected = c
	walletMu.Unlock()
}

// initWallet takes the --wallet flag, else hh.yaml, else the default operator.
func initWallet(cmd *cobra.Command) error {
	c := wallet.Choice{Kind: wallet.Default}
	if proj != nil && proj.Wallet.Kind != "" {
		c = wallet.Choice{Kind: proj.Wallet.Kind, FundHbar: proj.Wallet.Fund}
	}
	if v, _ := cmd.Flags().GetString("wallet"); v != "" {
		if v != wallet.Default && v != wallet.Burner {
			return fmt.Errorf("--wallet must be default or burner, import keys from the tui (ctrl+w)")
		}
		c.Kind = v
	}
	setWallet(c)
	return nil
}

// open connects to a network as the connected wallet.
func open(ctx context.Context, mode network.Mode) (*network.Target, func(), error) {
	return wallet.Open(ctx, mode, currentWallet())
}

// checkEnv funds the connected wallet for one round of checks when it is a
// burner, and exposes it to the check commands.
func checkEnv(mode network.Mode) func(ctx context.Context) ([]string, func(), error) {
	return func(ctx context.Context) ([]string, func(), error) {
		t, closeFn, err := open(ctx, mode)
		if err != nil {
			return nil, nil, err
		}
		return wallet.Env(t), closeFn, nil
	}
}

func walletCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wallet",
		Short: "Show the wallets a network offers and which one is connected",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := networkFlag(cmd, "")
			if err != nil {
				return err
			}
			cur := currentWallet()
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "\n  wallets on %s\n\n", mode)
			for _, info := range wallet.List(cmd.Context(), mode, cur) {
				dot := map[string]string{"ok": "●", "warn": "▲", "error": "×"}[info.Status]
				mark := " "
				if info.Kind == cur.Kind || (cur.Kind == "" && info.Kind == wallet.Default) {
					mark = "▸"
				}
				fmt.Fprintf(w, "  %s %s %-16s %-14s %-14s %s\n", mark, dot, info.Label, orNone(info.Account), orNone(info.Balance), info.Detail)
				if info.Note != "" {
					fmt.Fprintf(w, "        %s\n", info.Note)
				}
			}
			fmt.Fprintln(w, "\n  connect with --wallet default|burner, wallet: in hh.yaml, or ctrl+w in the tui")
			return nil
		},
	}
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func toTUIWallet(i wallet.Info) tui.Wallet {
	return tui.Wallet{Kind: i.Kind, Label: i.Label, Detail: i.Detail, Account: i.Account, KeyType: i.KeyType, Balance: i.Balance, Status: i.Status, Note: i.Note}
}

func tuiWallets(ctx context.Context, net string) ([]tui.Wallet, error) {
	mode, err := network.ParseMode(net)
	if err != nil {
		return nil, err
	}
	var out []tui.Wallet
	for _, info := range wallet.List(ctx, mode, currentWallet()) {
		out = append(out, toTUIWallet(info))
	}
	return out, nil
}

func tuiConnect(ctx context.Context, net string, choice tui.WalletChoice) (tui.Wallet, error) {
	mode, err := network.ParseMode(net)
	if err != nil {
		return tui.Wallet{}, err
	}
	c := wallet.Choice{Kind: choice.Kind, AccountID: choice.AccountID, Key: choice.Key}
	if c.Kind == wallet.Burner {
		c.FundHbar = currentWallet().FundHbar
	}
	info, err := wallet.Connect(ctx, mode, c)
	if err != nil {
		return toTUIWallet(info), err
	}
	setWallet(c)
	return toTUIWallet(info), nil
}
