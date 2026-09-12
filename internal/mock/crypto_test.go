package mock

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

func TestAccountCreateEd25519(t *testing.T) {
	h := newHarness(t)
	key, _ := hiero.PrivateKeyGenerateEd25519()

	receipt := h.run(hiero.NewAccountCreateTransaction().
		SetKeyWithoutAlias(key.PublicKey()).
		SetInitialBalance(hiero.NewHbar(5)).
		SetMaxAutomaticTokenAssociations(-1).
		SetAccountMemo("hello").
		Execute(h.client))
	if receipt.AccountID.Account != firstEntity {
		t.Fatalf("account id %v, want 0.0.%d", receipt.AccountID, firstEntity)
	}

	info, err := hiero.NewAccountInfoQuery().SetAccountID(*receipt.AccountID).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if info.Key.String() != key.PublicKey().String() || info.Balance.AsTinybar() != 5_0000_0000 {
		t.Fatalf("info key %v balance %v", info.Key, info.Balance)
	}

	var acct mirror.Account
	h.mustRest("/accounts/"+receipt.AccountID.String(), &acct)
	if acct.Key == nil || acct.Key.Type != "ED25519" || acct.Key.Key != key.PublicKey().StringRaw() {
		t.Fatalf("mirror key %+v", acct.Key)
	}
	if acct.Balance.Balance != 5_0000_0000 || acct.MaxAutomaticTokenAssociations != -1 || acct.Memo != "hello" {
		t.Fatalf("mirror account %+v", acct)
	}
	if acct.EvmAddress != "0x00000000000000000000000000000000000003e9" {
		t.Fatalf("evm address %s", acct.EvmAddress)
	}

	var op mirror.Account
	h.mustRest("/accounts/0.0.2", &op)
	if op.Balance.Balance != 1_000_000*1_0000_0000-5_0000_0000 {
		t.Fatalf("operator balance %d, fees should be zero", op.Balance.Balance)
	}
	if code := h.rest("/accounts/0.0.99999", nil); code != http.StatusNotFound {
		t.Fatalf("unknown account: http %d", code)
	}
	if code := h.rest("/accounts/not-an-id", nil); code != http.StatusBadRequest {
		t.Fatalf("bad id: http %d", code)
	}
}

func TestAccountCreateECDSAAlias(t *testing.T) {
	h := newHarness(t)
	key, _ := hiero.PrivateKeyGenerateEcdsa()

	unsigned, err := hiero.NewAccountCreateTransaction().
		SetECDSAKeyWithAlias(key.PublicKey()).
		Execute(h.client)
	expectStatus(t, h.status(unsigned, err), hiero.StatusInvalidSignature)

	tx, err := hiero.NewAccountCreateTransaction().
		SetECDSAKeyWithAlias(key.PublicKey()).
		SetInitialBalance(hiero.NewHbar(1)).
		FreezeWith(h.client)
	if err != nil {
		t.Fatal(err)
	}
	receipt := h.run(tx.Sign(key).Execute(h.client))

	evm := key.PublicKey().ToEvmAddress()
	var acct mirror.Account
	h.mustRest("/accounts/0x"+evm, &acct)
	if acct.Account != receipt.AccountID.String() || acct.EvmAddress != "0x"+evm {
		t.Fatalf("lookup by evm address: %+v", acct)
	}
	if acct.Key.Type != "ECDSA_SECP256K1" || acct.Alias == nil {
		t.Fatalf("key %+v alias %v", acct.Key, acct.Alias)
	}
}

func TestHbarTransfer(t *testing.T) {
	h := newHarness(t)
	alice := h.newAccount(10, 0)
	bob := h.newAccount(0, 0)

	transfer := func(amount float64, signers ...hiero.PrivateKey) (hiero.TransactionResponse, error) {
		tx, err := hiero.NewTransferTransaction().
			AddHbarTransfer(alice.id, hiero.NewHbar(-amount)).
			AddHbarTransfer(bob.id, hiero.NewHbar(amount)).
			FreezeWith(h.client)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range signers {
			tx.Sign(k)
		}
		return tx.Execute(h.client)
	}

	resp, err := transfer(4, alice.key)
	h.run(resp, err)
	expectStatus(t, h.status(transfer(100, alice.key)), hiero.StatusInsufficientAccountBalance)
	expectStatus(t, h.status(transfer(1)), hiero.StatusInvalidSignature)

	var acct mirror.Account
	h.mustRest("/accounts/"+bob.id.String(), &acct)
	if acct.Balance.Balance != 4_0000_0000 {
		t.Fatalf("bob balance %d", acct.Balance.Balance)
	}
	var balances balancesResponse
	h.mustRest("/balances?account.id="+alice.id.String(), &balances)
	if len(balances.Balances) != 1 || balances.Balances[0].Balance != 6_0000_0000 {
		t.Fatalf("balances %+v", balances)
	}

	var txs mirror.Transactions
	h.mustRest(mirrorTxPath(resp.TransactionID), &txs)
	if len(txs.Transactions) != 1 {
		t.Fatalf("transactions %+v", txs)
	}
	got := txs.Transactions[0]
	if got.Name != "CRYPTOTRANSFER" || got.Result != "SUCCESS" || len(got.Transfers) != 2 {
		t.Fatalf("mirror transaction %+v", got)
	}
	if got.Transfers[0].Account != alice.id.String() || got.Transfers[0].Amount != -4_0000_0000 {
		t.Fatalf("transfers %+v", got.Transfers)
	}
}

func TestThresholdKeyAccount(t *testing.T) {
	h := newHarness(t)
	var keys []hiero.PrivateKey
	threshold := hiero.KeyListWithThreshold(2)
	for range 3 {
		k, _ := hiero.PrivateKeyGenerateEd25519()
		keys = append(keys, k)
		threshold.Add(k.PublicKey())
	}
	receipt := h.run(hiero.NewAccountCreateTransaction().
		SetKeyWithoutAlias(threshold).
		SetInitialBalance(hiero.NewHbar(10)).
		Execute(h.client))
	multisig := *receipt.AccountID

	send := func(signers ...hiero.PrivateKey) hiero.Status {
		tx, err := hiero.NewTransferTransaction().
			AddHbarTransfer(multisig, hiero.NewHbar(-1)).
			AddHbarTransfer(h.operator, hiero.NewHbar(1)).
			FreezeWith(h.client)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range signers {
			tx.Sign(k)
		}
		return h.status(tx.Execute(h.client))
	}
	expectStatus(t, send(keys[0], keys[2]), hiero.StatusSuccess)
	expectStatus(t, send(keys[1]), hiero.StatusInvalidSignature)

	var acct mirror.Account
	h.mustRest("/accounts/"+multisig.String(), &acct)
	if acct.Key.Type != "ProtobufEncoded" || acct.Balance.Balance != 9_0000_0000 {
		t.Fatalf("mirror account %+v key %+v", acct, acct.Key)
	}
}

func TestPrechecks(t *testing.T) {
	h := newHarness(t)

	tx, err := hiero.NewTransferTransaction().
		AddHbarTransfer(h.operator, hiero.NewHbar(0)).
		SetTransactionID(hiero.TransactionIDGenerate(h.operator)).
		FreezeWith(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Execute(h.client); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Execute(h.client)
	var pre hiero.ErrHederaPreCheckStatus
	if !errors.As(err, &pre) || pre.Status != hiero.StatusDuplicateTransaction {
		t.Fatalf("resubmit: %v", err)
	}

	stranger, _ := hiero.PrivateKeyGenerateEd25519()
	h.client.SetOperator(h.operator, stranger)
	_, err = hiero.NewAccountCreateTransaction().SetKeyWithoutAlias(stranger.PublicKey()).Execute(h.client)
	if !errors.As(err, &pre) || pre.Status != hiero.StatusInvalidSignature {
		t.Fatalf("wrong payer key: %v", err)
	}
	h.client.SetOperator(h.operator, h.opKey)

	_, err = hiero.NewFileCreateTransaction().SetContents([]byte("x")).Execute(h.client)
	if !errors.As(err, &pre) || pre.Status != hiero.StatusNotSupported {
		t.Fatalf("file create: %v", err)
	}
	_, err = hiero.NewTokenNftInfoQuery().SetNftID(hiero.NftID{TokenID: hiero.TokenID{Token: 5}, SerialNumber: 1}).Execute(h.client)
	if err == nil || !strings.Contains(err.Error(), "NOT_SUPPORTED") {
		t.Fatalf("nft info query: %v", err)
	}
}

func TestAccountUpdateAndDelete(t *testing.T) {
	h := newHarness(t)
	alice := h.newAccount(3, 0)
	rotated, _ := hiero.PrivateKeyGenerateEd25519()

	update := func(keys ...hiero.PrivateKey) hiero.Status {
		tx := hiero.NewAccountUpdateTransaction().
			SetAccountID(alice.id).
			SetKey(rotated.PublicKey()).
			SetMaxAutomaticTokenAssociations(5)
		return h.status(signed(h, tx, keys...))
	}
	expectStatus(t, update(alice.key), hiero.StatusInvalidSignature)
	expectStatus(t, update(alice.key, rotated), hiero.StatusSuccess)

	var acct mirror.Account
	h.mustRest("/accounts/"+alice.id.String(), &acct)
	if acct.Key.Key != rotated.PublicKey().StringRaw() || acct.MaxAutomaticTokenAssociations != 5 {
		t.Fatalf("updated account %+v", acct)
	}

	del := hiero.NewAccountDeleteTransaction().SetAccountID(alice.id).SetTransferAccountID(h.operator)
	h.run(signed(h, del, rotated))
	h.mustRest("/accounts/"+alice.id.String(), &acct)
	if !acct.Deleted || acct.Balance.Balance != 0 {
		t.Fatalf("deleted account %+v", acct)
	}

	tx := hiero.NewTransferTransaction().
		AddHbarTransfer(h.operator, hiero.NewHbar(-1)).
		AddHbarTransfer(alice.id, hiero.NewHbar(1))
	expectStatus(t, h.status(tx.Execute(h.client)), hiero.StatusAccountDeleted)
}
