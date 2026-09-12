package mock

import (
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

func (h *harness) airdropFungible(token hiero.TokenID, from, to actor, amount int64) hiero.TransactionResponse {
	h.t.Helper()
	tx := hiero.NewTokenAirdropTransaction().
		AddTokenTransfer(token, from.id, -amount).
		AddTokenTransfer(token, to.id, amount)
	resp, err := signed(h, tx, from.key)
	h.run(resp, err)
	return resp
}

func (h *harness) pendingAirdrops(a actor, direction string) []mirror.TokenAirdrop {
	h.t.Helper()
	var out mirror.TokenAirdrops
	h.mustRest("/accounts/"+a.id.String()+"/airdrops/"+direction, &out)
	return out.Airdrops
}

func TestAirdropPendingThenClaim(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	alice := h.newAccount(0, -1)
	charlie := h.newAccount(0, 0)
	token := h.fungibleToken(treasury, nil)

	// alice has unlimited slots and gets the tokens straight away
	h.airdropFungible(token, treasury, alice, 10)
	if rel := h.relationship(alice, token); rel.Balance != 10 || !rel.AutomaticAssociation {
		t.Fatalf("alice relationship %+v", rel)
	}

	resp := h.airdropFungible(token, treasury, charlie, 100)
	record, err := resp.GetRecord(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.PendingAirdropRecords) != 1 || record.PendingAirdropRecords[0].GetPendingAirdropAmount() != 100 {
		t.Fatalf("pending airdrop records %+v", record.PendingAirdropRecords)
	}
	pendingID := record.PendingAirdropRecords[0].GetPendingAirdropId()
	h.airdropFungible(token, treasury, charlie, 50)

	pending := h.pendingAirdrops(charlie, "pending")
	if len(pending) != 1 || pending[0].Amount != 150 || pending[0].SenderID != treasury.id.String() || pending[0].SerialNumber != nil {
		t.Fatalf("pending for charlie %+v", pending)
	}
	if outstanding := h.pendingAirdrops(treasury, "outstanding"); len(outstanding) != 1 || outstanding[0].ReceiverID != charlie.id.String() {
		t.Fatalf("outstanding for treasury %+v", outstanding)
	}
	var tokens mirror.TokenRelationships
	h.mustRest("/accounts/"+charlie.id.String()+"/tokens", &tokens)
	if len(tokens.Tokens) != 0 {
		t.Fatalf("charlie should not be associated yet: %+v", tokens)
	}

	claim := func(keys ...hiero.PrivateKey) hiero.Status {
		return h.status(signed(h, hiero.NewTokenClaimAirdropTransaction().AddPendingAirdropId(pendingID), keys...))
	}
	expectStatus(t, claim(), hiero.StatusInvalidSignature)
	expectStatus(t, claim(charlie.key), hiero.StatusSuccess)
	expectStatus(t, claim(charlie.key), hiero.StatusInvalidPendingAirdropId)

	if rel := h.relationship(charlie, token); rel.Balance != 150 || rel.AutomaticAssociation {
		t.Fatalf("charlie after claim %+v", rel)
	}
	if left := h.pendingAirdrops(charlie, "pending"); len(left) != 0 {
		t.Fatalf("pending after claim %+v", left)
	}

	// reject sends the whole balance back to the treasury
	h.run(signed(h, hiero.NewTokenRejectTransaction().SetOwnerID(charlie.id).AddTokenID(token), charlie.key))
	if rel := h.relationship(treasury, token); rel.Balance != 10_000-10 {
		t.Fatalf("treasury after reject %+v", rel)
	}
}

func TestAirdropCancel(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	dave := h.newAccount(0, 0)

	create := hiero.NewTokenCreateTransaction().
		SetTokenName("Tickets").
		SetTokenSymbol("TIX").
		SetTokenType(hiero.TokenTypeNonFungibleUnique).
		SetTreasuryAccountID(treasury.id).
		SetSupplyKey(h.opKey.PublicKey())
	token := *h.run(signed(h, create, treasury.key)).TokenID
	h.run(hiero.NewTokenMintTransaction().SetTokenID(token).SetMetadata([]byte("seat 1")).Execute(h.client))

	nftID := hiero.NftID{TokenID: token, SerialNumber: 1}
	resp, err := signed(h, hiero.NewTokenAirdropTransaction().AddNftTransfer(nftID, treasury.id, dave.id), treasury.key)
	h.run(resp, err)
	record, err := resp.GetRecord(h.client)
	if err != nil {
		t.Fatal(err)
	}
	pendingID := record.PendingAirdropRecords[0].GetPendingAirdropId()
	if got := pendingID.GetNftID(); got == nil || got.SerialNumber != 1 {
		t.Fatalf("pending nft id %+v", pendingID)
	}

	pending := h.pendingAirdrops(dave, "pending")
	if len(pending) != 1 || pending[0].SerialNumber == nil || *pending[0].SerialNumber != 1 {
		t.Fatalf("pending nft airdrop %+v", pending)
	}

	cancel := func(keys ...hiero.PrivateKey) hiero.Status {
		return h.status(signed(h, hiero.NewTokenCancelAirdropTransaction().AddPendingAirdropId(pendingID), keys...))
	}
	expectStatus(t, cancel(), hiero.StatusInvalidSignature)
	expectStatus(t, cancel(treasury.key), hiero.StatusSuccess)
	expectStatus(t, cancel(treasury.key), hiero.StatusInvalidPendingAirdropId)

	if left := h.pendingAirdrops(dave, "pending"); len(left) != 0 {
		t.Fatalf("pending after cancel %+v", left)
	}
	var nft mirror.Nft
	h.mustRest("/tokens/"+token.String()+"/nfts/1", &nft)
	if nft.AccountID != treasury.id.String() {
		t.Fatalf("nft owner after cancel %s", nft.AccountID)
	}
}
