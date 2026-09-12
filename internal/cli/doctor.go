package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"github.com/spf13/cobra"

	"github.com/Gmin2/hedera-harness-go/internal/keys"
	"github.com/Gmin2/hedera-harness-go/internal/network"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that a network and operator are usable before spending anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := networkFlag(cmd, "")
			if err != nil {
				return err
			}
			d := &doctor{w: cmd.OutOrStdout()}
			d.run(cmd.Context(), mode)
			if d.failed {
				cmd.SilenceErrors = true
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
}

type doctor struct {
	w      io.Writer
	failed bool
}

func (d *doctor) ok(msg string, args ...any) {
	fmt.Fprintf(d.w, "  ✓ %s\n", fmt.Sprintf(msg, args...))
}
func (d *doctor) warn(msg string, args ...any) {
	fmt.Fprintf(d.w, "  ! %s\n", fmt.Sprintf(msg, args...))
}
func (d *doctor) bad(msg string, args ...any) {
	d.failed = true
	fmt.Fprintf(d.w, "  × %s\n", fmt.Sprintf(msg, args...))
}

func (d *doctor) run(ctx context.Context, mode network.Mode) {
	fmt.Fprintf(d.w, "\n  hh doctor · %s\n\n", mode)
	if _, err := os.Stat(".env"); err == nil {
		d.ok(".env loaded")
	}

	if mode == network.Mock {
		start := time.Now()
		t, closeTarget, err := openTarget(ctx, mode)
		if err != nil {
			d.bad("mock network did not start: %v", err)
			return
		}
		defer closeTarget()
		d.ok("mock network started in %s, operator %s", time.Since(start).Round(time.Millisecond), t.OperatorID)
		return
	}

	cfg, err := network.FromEnv(mode)
	if err != nil {
		d.bad("%v", err)
		return
	}
	d.ok("operator %s", cfg.OperatorID)

	if _, err := keys.Parse(cfg.OperatorKey, cfg.OperatorKeyType); err == keys.ErrAmbiguous {
		d.warn("operator key is raw hex without a type, hh will ask the mirror node which type it is")
	} else if err != nil {
		d.bad("operator key does not parse: %v", err)
		return
	}

	t, err := network.Connect(ctx, cfg)
	if err != nil {
		d.bad("%v", err)
		return
	}
	defer t.Close()
	d.ok("operator key parsed as %s", keys.Kind(t.OperatorKey))

	acct, src, err := t.Mirror.Account(ctx, cfg.OperatorID)
	if err != nil {
		d.bad("mirror node %s: %v", src, err)
		return
	}
	d.ok("mirror node reachable at %s", t.Mirror.Base())

	if acct.Key != nil {
		onChain := strings.ToLower(acct.Key.Key)
		pub := t.OperatorKey.PublicKey()
		if strings.HasSuffix(onChain, strings.ToLower(pub.StringRaw())) {
			d.ok("operator key matches the account key on chain (%s)", acct.Key.Type)
		} else {
			d.bad("operator key does not match account %s, every transaction would fail with INVALID_SIGNATURE", cfg.OperatorID)
		}
	}

	balance := float64(acct.Balance.Balance) / 1e8
	switch {
	case balance < 5:
		d.bad("operator balance is %.4f ℏ, scenarios that create tokens need more (portal.hedera.com/faucet)", balance)
	case balance < 50:
		d.warn("operator balance is %.4f ℏ, enough for a few scenarios", balance)
	default:
		d.ok("operator balance %.4f ℏ", balance)
	}

	pingDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				pingDone <- fmt.Errorf("%v", r)
			}
		}()
		pingDone <- t.Client.Ping(hiero.AccountID{Account: 3})
	}()
	select {
	case err := <-pingDone:
		if err != nil {
			d.bad("consensus node 0.0.3 did not answer: %v", err)
		} else {
			d.ok("consensus node 0.0.3 answered")
		}
	case <-time.After(20 * time.Second):
		d.bad("consensus node 0.0.3 did not answer within 20s")
	}
	fmt.Fprintln(d.w)
}
