package mock

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

type nftMove struct {
	token, serial, from, to int64
}

type relKey struct {
	account, token int64
}

// transfer is a set of balance changes that is validated as a whole and then
// applied in one go, so a failing check never leaves half a transfer behind.
type transfer struct {
	hbar      map[int64]int64
	tokens    map[int64]map[int64]int64 // token -> account -> delta
	nfts      []nftMove
	fees      []*services.AssessedCustomFee
	associate map[relKey]bool // explicit associations, used by airdrop claims
}

func newTransfer() *transfer {
	return &transfer{
		hbar:      map[int64]int64{},
		tokens:    map[int64]map[int64]int64{},
		associate: map[relKey]bool{},
	}
}

func (tr *transfer) addToken(token, account, amount int64) {
	if tr.tokens[token] == nil {
		tr.tokens[token] = map[int64]int64{}
	}
	tr.tokens[token][account] += amount
}

func newRelation(tok *token, automatic bool, at time.Time) *tokenRelation {
	rel := &tokenRelation{
		kyc:       services.TokenKycStatus_KycNotApplicable,
		freeze:    services.TokenFreezeStatus_FreezeNotApplicable,
		automatic: automatic,
		created:   at,
	}
	if tok.spec.KycKey != nil {
		rel.kyc = services.TokenKycStatus_Revoked
	}
	if tok.spec.FreezeKey != nil {
		rel.freeze = services.TokenFreezeStatus_Unfrozen
		if tok.spec.FreezeDefault {
			rel.freeze = services.TokenFreezeStatus_Frozen
		}
	}
	return rel
}

func hasFreeAutoSlot(a *account, planned int32) bool {
	return a.maxAutoAssociations == -1 || a.usedAutoAssociations+planned < a.maxAutoAssociations
}

// applyTransfer validates every change in tr against the current state and,
// only when all of them pass, applies them and fills the record.
func (l *ledger) applyTransfer(t *txn, tr *transfer) code {
	st := l.st

	for _, num := range slices.Sorted(maps.Keys(tr.hbar)) {
		a := st.accounts[num]
		if a.deleted {
			return services.ResponseCodeEnum_ACCOUNT_DELETED
		}
		if a.balance+tr.hbar[num] < 0 {
			return services.ResponseCodeEnum_INSUFFICIENT_ACCOUNT_BALANCE
		}
	}

	// nft moves change the serial count on both relationships
	counts := map[relKey]int64{}
	moved := map[[2]int64]bool{}
	for _, m := range tr.nfts {
		tok, c := st.token(tokenProto(m.token))
		if c != codeOK {
			return c
		}
		n := tok.nfts[m.serial]
		if n == nil {
			return services.ResponseCodeEnum_INVALID_NFT_ID
		}
		if n.owner != m.from || moved[[2]int64{m.token, m.serial}] {
			return services.ResponseCodeEnum_SENDER_DOES_NOT_OWN_NFT_SERIAL_NO
		}
		moved[[2]int64{m.token, m.serial}] = true
		counts[relKey{m.from, m.token}]--
		counts[relKey{m.to, m.token}]++
	}
	for tokNum, deltas := range tr.tokens {
		for acct, delta := range deltas {
			counts[relKey{acct, tokNum}] += delta
		}
	}

	touched := slices.SortedFunc(maps.Keys(counts), func(a, b relKey) int {
		return cmp.Or(cmp.Compare(a.token, b.token), cmp.Compare(a.account, b.account))
	})
	planned := map[relKey]*tokenRelation{}
	autoPlanned := map[int64]int32{}
	for _, k := range touched {
		delta := counts[k]
		tok, c := st.token(tokenProto(k.token))
		if c != codeOK {
			return c
		}
		a := st.accounts[k.account]
		if a.deleted {
			return services.ResponseCodeEnum_ACCOUNT_DELETED
		}
		if tok.paused {
			return services.ResponseCodeEnum_TOKEN_IS_PAUSED
		}
		rel := a.tokens[k.token]
		if rel == nil {
			switch {
			case delta < 0 || (delta == 0 && !tr.associate[k]):
				return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
			case tr.associate[k]:
				rel = newRelation(tok, false, t.rec.consensus)
			case hasFreeAutoSlot(a, autoPlanned[a.num]):
				rel = newRelation(tok, true, t.rec.consensus)
				autoPlanned[a.num]++
			case a.maxAutoAssociations == 0:
				return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
			default:
				return services.ResponseCodeEnum_NO_REMAINING_AUTOMATIC_ASSOCIATIONS
			}
			planned[k] = rel
		}
		if tok.spec.KycKey != nil && rel.kyc != services.TokenKycStatus_Granted {
			return services.ResponseCodeEnum_ACCOUNT_KYC_NOT_GRANTED_FOR_TOKEN
		}
		if rel.freeze == services.TokenFreezeStatus_Frozen {
			return services.ResponseCodeEnum_ACCOUNT_FROZEN_FOR_TOKEN
		}
		if rel.balance+delta < 0 {
			return services.ResponseCodeEnum_INSUFFICIENT_TOKEN_BALANCE
		}
	}

	for num, delta := range tr.hbar {
		st.accounts[num].balance += delta
	}
	for _, k := range touched {
		a := st.accounts[k.account]
		if rel, isNew := planned[k]; isNew {
			a.tokens[k.token] = rel
			if rel.automatic {
				a.usedAutoAssociations++
				t.rec.autoAssociations = append(t.rec.autoAssociations, &services.TokenAssociation{
					TokenId:   tokenProto(k.token),
					AccountId: accountProto(k.account),
				})
			}
		}
		a.tokens[k.token].balance += counts[k]
	}
	for _, m := range tr.nfts {
		n := st.tokens[m.token].nfts[m.serial]
		n.owner = m.to
		n.modified = t.rec.consensus
	}

	t.rec.transfers = hbarTransfers(tr.hbar)
	t.rec.tokenTransfers = tokenTransferLists(tr)
	t.rec.assessedFees = tr.fees
	return codeOK
}

// hbarTransfers renders deltas sorted by account, dropping zero entries.
func hbarTransfers(deltas map[int64]int64) []*services.AccountAmount {
	var out []*services.AccountAmount
	for _, num := range slices.Sorted(maps.Keys(deltas)) {
		if deltas[num] != 0 {
			out = append(out, &services.AccountAmount{AccountID: accountProto(num), Amount: deltas[num]})
		}
	}
	return out
}

func tokenTransferLists(tr *transfer) []*services.TokenTransferList {
	lists := map[int64]*services.TokenTransferList{}
	get := func(token int64) *services.TokenTransferList {
		if lists[token] == nil {
			lists[token] = &services.TokenTransferList{Token: tokenProto(token)}
		}
		return lists[token]
	}
	for token, deltas := range tr.tokens {
		if amounts := hbarTransfers(deltas); len(amounts) > 0 {
			get(token).Transfers = amounts
		}
	}
	for _, m := range tr.nfts {
		list := get(m.token)
		list.NftTransfers = append(list.NftTransfers, &services.NftTransfer{
			SenderAccountID:   accountProto(m.from),
			ReceiverAccountID: accountProto(m.to),
			SerialNumber:      m.serial,
		})
	}
	var out []*services.TokenTransferList
	for _, token := range slices.Sorted(maps.Keys(lists)) {
		out = append(out, lists[token])
	}
	return out
}
