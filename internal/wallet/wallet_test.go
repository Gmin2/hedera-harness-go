package wallet

import (
	"context"
	"strings"
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/network"
)

func TestBurnerOnMock(t *testing.T) {
	ctx := context.Background()
	tgt, closeFn, err := Open(ctx, network.Mock, Choice{Kind: Burner, FundHbar: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if tgt.OperatorID.Account < 1001 {
		t.Fatalf("burner should be a fresh account, got %s", tgt.OperatorID)
	}
	acct, _, err := tgt.Mirror.Account(ctx, tgt.OperatorID.String())
	if err != nil {
		t.Fatal(err)
	}
	if acct.Balance.Balance != 7*100_000_000 {
		t.Fatalf("burner balance %d", acct.Balance.Balance)
	}

	env := strings.Join(Env(tgt), "\n")
	for _, want := range []string{"HH_OPERATOR_ID=" + tgt.OperatorID.String(), "__RUNTIME_DEPLOYER_PRIVATE_KEY=0x", "HH_NETWORK=mock"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %s:\n%s", want, env)
		}
	}
}

func TestConnectOnMock(t *testing.T) {
	ctx := context.Background()
	info, err := Connect(ctx, network.Mock, Choice{})
	if err != nil || info.Account != "0.0.2" || info.Status != "ok" {
		t.Fatalf("default: %+v %v", info, err)
	}
	if _, err := Connect(ctx, network.Mock, Choice{Kind: Import, AccountID: "0.0.5", Key: "x"}); err == nil {
		t.Fatal("import on mock should be refused")
	}
	list := List(ctx, network.Mock, Choice{Kind: Burner, FundHbar: 3})
	if len(list) != 2 || list[1].Kind != Burner || !strings.Contains(list[1].Detail, "3 ℏ") {
		t.Fatalf("list: %+v", list)
	}
}

func TestFormatHbar(t *testing.T) {
	for in, want := range map[int64]string{100_000_000_000_000: "1,000,000 ℏ", 9_744_000_000: "97.44 ℏ", 1_234_500: "0.0123 ℏ"} {
		if got := formatHbar(in); got != want {
			t.Errorf("%d: got %s want %s", in, got, want)
		}
	}
}
