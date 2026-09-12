package mock

import (
	"net/http"
	"slices"
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// signed freezes tx, adds the signatures and executes it.
func signed[T any, P interface {
	*T
	FreezeWith(*hiero.Client) (P, error)
	Sign(hiero.PrivateKey) P
	Execute(*hiero.Client) (hiero.TransactionResponse, error)
}](h *harness, tx P, keys ...hiero.PrivateKey) (hiero.TransactionResponse, error) {
	h.t.Helper()
	frozen, err := tx.FreezeWith(h.client)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, k := range keys {
		frozen.Sign(k)
	}
	return frozen.Execute(h.client)
}

func (h *harness) fungibleToken(treasury actor, configure func(*hiero.TokenCreateTransaction)) hiero.TokenID {
	h.t.Helper()
	tx := hiero.NewTokenCreateTransaction().
		SetTokenName("Test Token").
		SetTokenSymbol("TT").
		SetDecimals(2).
		SetInitialSupply(10_000).
		SetTreasuryAccountID(treasury.id).
		SetAdminKey(h.opKey.PublicKey())
	if configure != nil {
		configure(tx)
	}
	receipt := h.run(signed(h, tx, treasury.key))
	return *receipt.TokenID
}

func (h *harness) associate(a actor, tokens ...hiero.TokenID) {
	h.t.Helper()
	h.run(signed(h, hiero.NewTokenAssociateTransaction().SetAccountID(a.id).SetTokenIDs(tokens...), a.key))
}

func (h *harness) sendToken(token hiero.TokenID, from, to actor, amount int64) hiero.Status {
	h.t.Helper()
	tx := hiero.NewTransferTransaction().
		AddTokenTransfer(token, from.id, -amount).
		AddTokenTransfer(token, to.id, amount)
	return h.status(signed(h, tx, from.key))
}

func (h *harness) relationship(a actor, token hiero.TokenID) mirror.TokenRelationship {
	h.t.Helper()
	var rels mirror.TokenRelationships
	h.mustRest("/accounts/"+a.id.String()+"/tokens?token.id="+token.String(), &rels)
	if len(rels.Tokens) != 1 {
		h.t.Fatalf("relationships of %s for %s: %+v", a.id, token, rels)
	}
	return rels.Tokens[0]
}

func TestTokenKycGate(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	alice := h.newAccount(10, 0)
	kyc, _ := hiero.PrivateKeyGenerateEd25519()

	token := h.fungibleToken(treasury, func(tx *hiero.TokenCreateTransaction) {
		tx.SetKycKey(kyc.PublicKey())
	})
	if treasuryRel := h.relationship(treasury, token); treasuryRel.KycStatus != "GRANTED" || treasuryRel.Balance != 10_000 {
		t.Fatalf("treasury relationship %+v", treasuryRel)
	}

	expectStatus(t, h.sendToken(token, treasury, alice, 100), hiero.StatusTokenNotAssociatedToAccount)
	h.associate(alice, token)
	expectStatus(t, h.status(signed(h, hiero.NewTokenAssociateTransaction().SetAccountID(alice.id).SetTokenIDs(token), alice.key)),
		hiero.StatusTokenAlreadyAssociatedToAccount)
	if rel := h.relationship(alice, token); rel.KycStatus != "REVOKED" || rel.FreezeStatus != "NOT_APPLICABLE" {
		t.Fatalf("fresh relationship %+v", rel)
	}

	expectStatus(t, h.sendToken(token, treasury, alice, 100), hiero.StatusAccountKycNotGrantedForToken)
	expectStatus(t, h.status(signed(h, hiero.NewTokenGrantKycTransaction().SetTokenID(token).SetAccountID(alice.id))),
		hiero.StatusInvalidSignature)
	h.run(signed(h, hiero.NewTokenGrantKycTransaction().SetTokenID(token).SetAccountID(alice.id), kyc))
	expectStatus(t, h.sendToken(token, treasury, alice, 100), hiero.StatusSuccess)

	rel := h.relationship(alice, token)
	if rel.KycStatus != "GRANTED" || rel.Balance != 100 || rel.Decimals != 2 {
		t.Fatalf("relationship after grant %+v", rel)
	}
	var tok mirror.Token
	h.mustRest("/tokens/"+token.String(), &tok)
	if tok.TotalSupply != "10000" || tok.KycKey == nil || tok.TreasuryAccountID != treasury.id.String() {
		t.Fatalf("token %+v", tok)
	}
}

func TestFreezeAndPauseGates(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	alice := h.newAccount(10, 0)

	token := h.fungibleToken(treasury, func(tx *hiero.TokenCreateTransaction) {
		tx.SetFreezeKey(h.opKey.PublicKey()).
			SetPauseKey(h.opKey.PublicKey()).
			SetSupplyKey(h.opKey.PublicKey())
	})
	h.associate(alice, token)
	if rel := h.relationship(alice, token); rel.FreezeStatus != "UNFROZEN" || rel.KycStatus != "NOT_APPLICABLE" {
		t.Fatalf("relationship %+v", rel)
	}

	h.run(hiero.NewTokenFreezeTransaction().SetTokenID(token).SetAccountID(alice.id).Execute(h.client))
	expectStatus(t, h.sendToken(token, treasury, alice, 5), hiero.StatusAccountFrozenForToken)
	h.run(hiero.NewTokenUnfreezeTransaction().SetTokenID(token).SetAccountID(alice.id).Execute(h.client))
	expectStatus(t, h.sendToken(token, treasury, alice, 5), hiero.StatusSuccess)

	h.run(hiero.NewTokenPauseTransaction().SetTokenID(token).Execute(h.client))
	var tok mirror.Token
	h.mustRest("/tokens/"+token.String(), &tok)
	if tok.PauseStatus != "PAUSED" {
		t.Fatalf("pause status %s", tok.PauseStatus)
	}
	expectStatus(t, h.sendToken(token, alice, treasury, 1), hiero.StatusTokenIsPaused)
	expectStatus(t, h.status(hiero.NewTokenMintTransaction().SetTokenID(token).SetAmount(1).Execute(h.client)), hiero.StatusTokenIsPaused)
	h.run(hiero.NewTokenUnpauseTransaction().SetTokenID(token).Execute(h.client))
	expectStatus(t, h.sendToken(token, alice, treasury, 1), hiero.StatusSuccess)

	mint := h.run(hiero.NewTokenMintTransaction().SetTokenID(token).SetAmount(500).Execute(h.client))
	if mint.TotalSupply != 10_500 {
		t.Fatalf("total supply %d", mint.TotalSupply)
	}

	noKeys := h.fungibleToken(treasury, nil)
	expectStatus(t, h.status(hiero.NewTokenPauseTransaction().SetTokenID(noKeys).Execute(h.client)), hiero.StatusTokenHasNoPauseKey)
	expectStatus(t, h.status(hiero.NewTokenFreezeTransaction().SetTokenID(noKeys).SetAccountID(treasury.id).Execute(h.client)), hiero.StatusTokenHasNoFreezeKey)
}

func TestAutoAssociationSlots(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	oneSlot := h.newAccount(0, 1)
	noSlots := h.newAccount(0, 0)

	first := h.fungibleToken(treasury, nil)
	second := h.fungibleToken(treasury, nil)

	expectStatus(t, h.sendToken(first, treasury, oneSlot, 10), hiero.StatusSuccess)
	expectStatus(t, h.sendToken(second, treasury, oneSlot, 10), hiero.StatusNoRemainingAutomaticAssociations)
	expectStatus(t, h.sendToken(first, treasury, noSlots, 10), hiero.StatusTokenNotAssociatedToAccount)

	if rel := h.relationship(oneSlot, first); !rel.AutomaticAssociation || rel.Balance != 10 {
		t.Fatalf("auto relationship %+v", rel)
	}
	var acct mirror.Account
	h.mustRest("/accounts/"+oneSlot.id.String(), &acct)
	if len(acct.Balance.Tokens) != 1 || acct.Balance.Tokens[0].TokenID != first.String() {
		t.Fatalf("account token balances %+v", acct.Balance.Tokens)
	}
}

func TestNftMintAndTransfer(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	bob := h.newAccount(10, 0)
	supply, _ := hiero.PrivateKeyGenerateEd25519()

	create := hiero.NewTokenCreateTransaction().
		SetTokenName("Art").
		SetTokenSymbol("ART").
		SetTokenType(hiero.TokenTypeNonFungibleUnique).
		SetSupplyType(hiero.TokenSupplyTypeFinite).
		SetMaxSupply(3).
		SetTreasuryAccountID(treasury.id).
		SetSupplyKey(supply.PublicKey())
	token := *h.run(signed(h, create, treasury.key)).TokenID

	mint := hiero.NewTokenMintTransaction().SetTokenID(token).
		SetMetadatas([][]byte{[]byte("a"), []byte("b"), []byte("c")})
	expectStatus(t, h.status(mint.Execute(h.client)), hiero.StatusInvalidSignature)
	receipt := h.run(signed(h, hiero.NewTokenMintTransaction().SetTokenID(token).
		SetMetadatas([][]byte{[]byte("a"), []byte("b"), []byte("c")}), supply))
	if !slices.Equal(receipt.SerialNumbers, []int64{1, 2, 3}) || receipt.TotalSupply != 3 {
		t.Fatalf("mint receipt serials %v supply %d", receipt.SerialNumbers, receipt.TotalSupply)
	}
	expectStatus(t, h.status(signed(h, hiero.NewTokenMintTransaction().SetTokenID(token).SetMetadata([]byte("d")), supply)),
		hiero.StatusTokenMaxSupplyReached)

	h.associate(bob, token)
	nftTransfer := func(serial int64) hiero.Status {
		tx := hiero.NewTransferTransaction().AddNftTransfer(hiero.NftID{TokenID: token, SerialNumber: serial}, treasury.id, bob.id)
		return h.status(signed(h, tx, treasury.key))
	}
	expectStatus(t, nftTransfer(2), hiero.StatusSuccess)
	expectStatus(t, nftTransfer(2), hiero.StatusSenderDoesNotOwnNftSerialNo)

	var one mirror.Nft
	h.mustRest("/tokens/"+token.String()+"/nfts/2", &one)
	if one.AccountID != bob.id.String() || one.Metadata != "Yg==" {
		t.Fatalf("nft 2 %+v", one)
	}
	if code := h.rest("/tokens/"+token.String()+"/nfts/9", nil); code != http.StatusNotFound {
		t.Fatalf("missing serial: http %d", code)
	}

	var owned mirror.Nfts
	h.mustRest("/accounts/"+bob.id.String()+"/nfts", &owned)
	if len(owned.Nfts) != 1 || owned.Nfts[0].SerialNumber != 2 {
		t.Fatalf("bob nfts %+v", owned)
	}

	// walk all serials two at a time, newest first
	var serials []int64
	path := "/tokens/" + token.String() + "/nfts?limit=2"
	for pages := 0; path != ""; pages++ {
		if pages > 3 {
			t.Fatal("pagination does not terminate")
		}
		var page mirror.Nfts
		h.mustRest(path, &page)
		for _, n := range page.Nfts {
			serials = append(serials, n.SerialNumber)
		}
		path = ""
		if page.Links.Next != nil {
			path = (*page.Links.Next)[len("/api/v1"):]
		}
	}
	if !slices.Equal(serials, []int64{3, 2, 1}) {
		t.Fatalf("paged serials %v", serials)
	}
}

func TestCustomFees(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	collector := h.newAccount(0, 0)
	alice := h.newAccount(10, 0)
	bob := h.newAccount(10, 0)

	fixed := hiero.NewCustomFixedFee().
		SetHbarAmount(hiero.HbarFromTinybar(1000)).
		SetFeeCollectorAccountID(collector.id)
	fractional := hiero.NewCustomFractionalFee().
		SetNumerator(1).
		SetDenominator(10).
		SetMin(1).
		SetFeeCollectorAccountID(collector.id)
	token := h.fungibleToken(treasury, func(tx *hiero.TokenCreateTransaction) {
		tx.SetCustomFees([]hiero.Fee{fixed, fractional})
	})
	h.associate(alice, token)
	h.associate(bob, token)

	// the treasury is exempt
	expectStatus(t, h.sendToken(token, treasury, alice, 1000), hiero.StatusSuccess)

	tx := hiero.NewTransferTransaction().
		AddTokenTransfer(token, alice.id, -500).
		AddTokenTransfer(token, bob.id, 500)
	resp, err := signed(h, tx, alice.key)
	h.run(resp, err)

	record, err := resp.GetRecord(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.AssessedCustomFees) != 2 {
		t.Fatalf("record assessed fees %+v", record.AssessedCustomFees)
	}

	if rel := h.relationship(bob, token); rel.Balance != 450 {
		t.Fatalf("bob received %d, want 450 after the 10%% fee", rel.Balance)
	}
	if rel := h.relationship(collector, token); rel.Balance != 50 {
		t.Fatalf("collector token balance %d", rel.Balance)
	}
	var coll mirror.Account
	h.mustRest("/accounts/"+collector.id.String(), &coll)
	if coll.Balance.Balance != 1000 {
		t.Fatalf("collector hbar %d", coll.Balance.Balance)
	}
	var al mirror.Account
	h.mustRest("/accounts/"+alice.id.String(), &al)
	if al.Balance.Balance != 10_0000_0000-1000 {
		t.Fatalf("alice hbar %d", al.Balance.Balance)
	}

	var txs mirror.Transactions
	h.mustRest(mirrorTxPath(resp.TransactionID), &txs)
	fees := txs.Transactions[0].AssessedCustomFees
	if len(fees) != 2 {
		t.Fatalf("mirror assessed fees %+v", fees)
	}
	if fees[0].TokenID != nil || fees[0].Amount != 1000 || fees[0].EffectivePayerAccountIDs[0] != alice.id.String() {
		t.Fatalf("fixed fee %+v", fees[0])
	}
	if fees[1].TokenID == nil || *fees[1].TokenID != token.String() || fees[1].Amount != 50 ||
		fees[1].CollectorAccountID != collector.id.String() || fees[1].EffectivePayerAccountIDs[0] != bob.id.String() {
		t.Fatalf("fractional fee %+v", fees[1])
	}

	var tok mirror.Token
	h.mustRest("/tokens/"+token.String(), &tok)
	if len(tok.CustomFees.FixedFees) != 1 || len(tok.CustomFees.FractionalFees) != 1 {
		t.Fatalf("token custom fees %+v", tok.CustomFees)
	}
}

func TestRoyaltyFee(t *testing.T) {
	h := newHarness(t)
	artist := h.newAccount(10, 0)
	collector := h.newAccount(0, 0)
	alice := h.newAccount(20, -1)
	bob := h.newAccount(20, -1)

	royalty := hiero.NewCustomRoyaltyFee().
		SetNumerator(1).
		SetDenominator(10).
		SetFallbackFee(hiero.NewCustomFixedFee().SetHbarAmount(hiero.NewHbar(5))).
		SetFeeCollectorAccountID(collector.id)
	create := hiero.NewTokenCreateTransaction().
		SetTokenName("Prints").
		SetTokenSymbol("PRT").
		SetTokenType(hiero.TokenTypeNonFungibleUnique).
		SetTreasuryAccountID(artist.id).
		SetSupplyKey(h.opKey.PublicKey()).
		SetCustomFees([]hiero.Fee{royalty})
	token := *h.run(signed(h, create, artist.key)).TokenID
	h.run(hiero.NewTokenMintTransaction().SetTokenID(token).SetMetadata([]byte("print")).Execute(h.client))
	nftID := hiero.NftID{TokenID: token, SerialNumber: 1}

	hbarOf := func(a actor) int64 {
		var acct mirror.Account
		h.mustRest("/accounts/"+a.id.String(), &acct)
		return acct.Balance.Balance
	}

	// the treasury is exempt
	h.run(signed(h, hiero.NewTransferTransaction().AddNftTransfer(nftID, artist.id, alice.id), artist.key))
	if got := hbarOf(collector); got != 0 {
		t.Fatalf("collector charged on a treasury transfer: %d", got)
	}

	// a gift moves no value, so the receiver pays the fallback fee
	h.run(signed(h, hiero.NewTransferTransaction().AddNftTransfer(nftID, alice.id, bob.id), alice.key))
	if hbarOf(collector) != 5_0000_0000 || hbarOf(bob) != 15_0000_0000 {
		t.Fatalf("fallback fee: collector %d bob %d", hbarOf(collector), hbarOf(bob))
	}

	// a sale pays a tenth of the price to the collector out of the seller proceeds
	sale := hiero.NewTransferTransaction().
		AddNftTransfer(nftID, bob.id, alice.id).
		AddHbarTransfer(alice.id, hiero.NewHbar(-10)).
		AddHbarTransfer(bob.id, hiero.NewHbar(10))
	resp, err := signed(h, sale, alice.key, bob.key)
	h.run(resp, err)
	if hbarOf(collector) != 6_0000_0000 || hbarOf(bob) != 24_0000_0000 {
		t.Fatalf("royalty: collector %d bob %d", hbarOf(collector), hbarOf(bob))
	}
	var txs mirror.Transactions
	h.mustRest(mirrorTxPath(resp.TransactionID), &txs)
	if fees := txs.Transactions[0].AssessedCustomFees; len(fees) != 1 || fees[0].Amount != 1_0000_0000 {
		t.Fatalf("assessed royalty %+v", fees)
	}
}

func TestBurnAndDissociate(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	alice := h.newAccount(10, 0)
	token := h.fungibleToken(treasury, func(tx *hiero.TokenCreateTransaction) {
		tx.SetSupplyKey(h.opKey.PublicKey())
	})

	burn := h.run(hiero.NewTokenBurnTransaction().SetTokenID(token).SetAmount(400).Execute(h.client))
	if burn.TotalSupply != 9_600 {
		t.Fatalf("supply after burn %d", burn.TotalSupply)
	}
	expectStatus(t, h.status(hiero.NewTokenBurnTransaction().SetTokenID(token).SetAmount(1_000_000).Execute(h.client)),
		hiero.StatusInsufficientTokenBalance)

	dissociate := func(a actor) hiero.Status {
		return h.status(signed(h, hiero.NewTokenDissociateTransaction().SetAccountID(a.id).SetTokenIDs(token), a.key))
	}
	expectStatus(t, dissociate(treasury), hiero.StatusAccountIsTreasury)
	h.associate(alice, token)
	expectStatus(t, h.sendToken(token, treasury, alice, 1), hiero.StatusSuccess)
	expectStatus(t, dissociate(alice), hiero.StatusTransactionRequiresZeroTokenBalances)
	expectStatus(t, h.sendToken(token, alice, treasury, 1), hiero.StatusSuccess)
	expectStatus(t, dissociate(alice), hiero.StatusSuccess)
	expectStatus(t, dissociate(alice), hiero.StatusTokenNotAssociatedToAccount)
}
