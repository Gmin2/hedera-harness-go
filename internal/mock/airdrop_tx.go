package mock

import (
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

const maxPendingAirdropIDs = 10

func (l *ledger) airdropKeys(b *services.TokenAirdropTransactionBody) ([]*services.Key, code) {
	var keys []*services.Key
	for _, list := range b.TokenTransfers {
		for _, aa := range list.Transfers {
			if aa.Amount >= 0 {
				continue
			}
			a, c := l.st.account(aa.AccountID)
			if c != codeOK {
				return nil, c
			}
			keys = append(keys, a.key)
		}
		for _, n := range list.NftTransfers {
			a, c := l.st.account(n.SenderAccountID)
			if c != codeOK {
				return nil, c
			}
			keys = append(keys, a.key)
		}
	}
	return keys, codeOK
}

// airdrop sends tokens straight to receivers that are associated or have a
// free auto association slot, and parks the rest as pending airdrops.
func (l *ledger) airdrop(t *txn, b *services.TokenAirdropTransactionBody) code {
	if len(b.TokenTransfers) == 0 {
		return services.ResponseCodeEnum_EMPTY_TOKEN_TRANSFER_BODY
	}

	tr := newTransfer()
	var pending []*pendingAirdrop
	slots := map[int64]int32{}
	direct := func(tok *token, receiver *account) bool {
		if receiver.tokens[tok.num] != nil {
			return true
		}
		if hasFreeAutoSlot(receiver, slots[receiver.num]) {
			slots[receiver.num]++
			return true
		}
		return false
	}

	seen := map[int64]bool{}
	for _, list := range b.TokenTransfers {
		tok, c := l.st.token(list.Token)
		if c != codeOK {
			return c
		}
		if seen[tok.num] {
			return services.ResponseCodeEnum_TOKEN_ID_REPEATED_IN_TOKEN_LIST
		}
		seen[tok.num] = true
		if tok.paused {
			return services.ResponseCodeEnum_TOKEN_IS_PAUSED
		}

		// senders keep their full debit for direct legs, pending legs are
		// taken back out of the first senders in list order
		type debit struct {
			num  int64
			left int64
		}
		var senders []*debit
		var credits []*services.AccountAmount
		var sum int64
		for _, aa := range list.Transfers {
			if aa.IsApproval {
				return services.ResponseCodeEnum_NOT_SUPPORTED
			}
			a, c := l.st.liveAccount(aa.AccountID)
			if c != codeOK {
				return c
			}
			sum += aa.Amount
			if aa.Amount < 0 {
				senders = append(senders, &debit{num: a.num, left: -aa.Amount})
				tr.addToken(tok.num, a.num, aa.Amount)
			} else if aa.Amount > 0 {
				credits = append(credits, aa)
			}
		}
		if sum != 0 {
			return services.ResponseCodeEnum_TRANSFERS_NOT_ZERO_SUM_FOR_TOKEN
		}
		for _, aa := range credits {
			receiver, _ := l.st.liveAccount(aa.AccountID)
			if direct(tok, receiver) {
				tr.addToken(tok.num, receiver.num, aa.Amount)
				continue
			}
			need := aa.Amount
			for _, s := range senders {
				take := min(need, s.left)
				if take == 0 {
					continue
				}
				s.left -= take
				need -= take
				tr.addToken(tok.num, s.num, take)
				pending = append(pending, &pendingAirdrop{
					airdropKey: airdropKey{sender: s.num, receiver: receiver.num, token: tok.num},
					amount:     take,
				})
			}
		}

		for _, n := range list.NftTransfers {
			if n.IsApproval {
				return services.ResponseCodeEnum_NOT_SUPPORTED
			}
			from, c := l.st.liveAccount(n.SenderAccountID)
			if c != codeOK {
				return c
			}
			to, c := l.st.liveAccount(n.ReceiverAccountID)
			if c != codeOK {
				return c
			}
			item := tok.nfts[n.SerialNumber]
			if item == nil {
				return services.ResponseCodeEnum_INVALID_NFT_ID
			}
			if item.owner != from.num {
				return services.ResponseCodeEnum_SENDER_DOES_NOT_OWN_NFT_SERIAL_NO
			}
			if direct(tok, to) {
				tr.nfts = append(tr.nfts, nftMove{token: tok.num, serial: n.SerialNumber, from: from.num, to: to.num})
				continue
			}
			key := airdropKey{sender: from.num, receiver: to.num, token: tok.num, serial: n.SerialNumber}
			if l.st.airdrops[key] != nil {
				return services.ResponseCodeEnum_PENDING_NFT_AIRDROP_ALREADY_EXISTS
			}
			pending = append(pending, &pendingAirdrop{airdropKey: key})
		}
	}

	for _, p := range pending {
		if l.st.accounts[p.sender].tokens[p.token] == nil {
			return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
		}
	}
	if c := l.assessCustomFees(tr); c != codeOK {
		return c
	}
	if c := l.applyTransfer(t, tr); c != codeOK {
		return c
	}

	for _, p := range pending {
		p.created = t.rec.consensus
		if existing := l.st.airdrops[p.airdropKey]; existing != nil {
			existing.amount += p.amount
		} else {
			l.st.airdrops[p.airdropKey] = p
		}
		rec := &services.PendingAirdropRecord{PendingAirdropId: pendingAirdropProto(p.airdropKey)}
		if p.serial == 0 {
			rec.PendingAirdropValue = &services.PendingAirdropValue{Amount: uint64(p.amount)}
		}
		t.rec.pendingAirdrops = append(t.rec.pendingAirdrops, rec)
	}
	return codeOK
}

func pendingAirdropProto(k airdropKey) *services.PendingAirdropId {
	id := &services.PendingAirdropId{SenderId: accountProto(k.sender), ReceiverId: accountProto(k.receiver)}
	if k.serial == 0 {
		id.TokenReference = &services.PendingAirdropId_FungibleTokenType{FungibleTokenType: tokenProto(k.token)}
	} else {
		id.TokenReference = &services.PendingAirdropId_NonFungibleToken{
			NonFungibleToken: &services.NftID{Token_ID: tokenProto(k.token), SerialNumber: k.serial},
		}
	}
	return id
}

func (l *ledger) pendingKey(id *services.PendingAirdropId) (airdropKey, code) {
	sender, c := l.st.account(id.GetSenderId())
	if c != codeOK {
		return airdropKey{}, services.ResponseCodeEnum_INVALID_PENDING_AIRDROP_ID
	}
	receiver, c := l.st.account(id.GetReceiverId())
	if c != codeOK {
		return airdropKey{}, services.ResponseCodeEnum_INVALID_PENDING_AIRDROP_ID
	}
	k := airdropKey{sender: sender.num, receiver: receiver.num}
	switch ref := id.GetTokenReference().(type) {
	case *services.PendingAirdropId_FungibleTokenType:
		k.token = ref.FungibleTokenType.GetTokenNum()
	case *services.PendingAirdropId_NonFungibleToken:
		k.token = ref.NonFungibleToken.GetToken_ID().GetTokenNum()
		k.serial = ref.NonFungibleToken.GetSerialNumber()
	default:
		return airdropKey{}, services.ResponseCodeEnum_INVALID_PENDING_AIRDROP_ID
	}
	return k, codeOK
}

// pendingAirdropKeys requires the receivers for claims and the senders for cancels.
func (l *ledger) pendingAirdropKeys(ids []*services.PendingAirdropId, receivers bool) ([]*services.Key, code) {
	var keys []*services.Key
	for _, id := range ids {
		acct := id.GetSenderId()
		if receivers {
			acct = id.GetReceiverId()
		}
		a, c := l.st.account(acct)
		if c != codeOK {
			return nil, services.ResponseCodeEnum_INVALID_PENDING_AIRDROP_ID
		}
		keys = append(keys, a.key)
	}
	return keys, codeOK
}

func (l *ledger) lookupPending(ids []*services.PendingAirdropId) ([]*pendingAirdrop, code) {
	switch {
	case len(ids) == 0:
		return nil, services.ResponseCodeEnum_EMPTY_PENDING_AIRDROP_ID_LIST
	case len(ids) > maxPendingAirdropIDs:
		return nil, services.ResponseCodeEnum_PENDING_AIRDROP_ID_LIST_TOO_LONG
	}
	var out []*pendingAirdrop
	seen := map[airdropKey]bool{}
	for _, id := range ids {
		k, c := l.pendingKey(id)
		if c != codeOK {
			return nil, c
		}
		if seen[k] {
			return nil, services.ResponseCodeEnum_PENDING_AIRDROP_ID_REPEATED
		}
		seen[k] = true
		p := l.st.airdrops[k]
		if p == nil {
			return nil, services.ResponseCodeEnum_INVALID_PENDING_AIRDROP_ID
		}
		out = append(out, p)
	}
	return out, codeOK
}

func (l *ledger) claimAirdrops(t *txn, ids []*services.PendingAirdropId) code {
	pending, c := l.lookupPending(ids)
	if c != codeOK {
		return c
	}
	tr := newTransfer()
	for _, p := range pending {
		if _, c := l.st.token(tokenProto(p.token)); c != codeOK {
			return c
		}
		if p.serial == 0 {
			tr.addToken(p.token, p.sender, -p.amount)
			tr.addToken(p.token, p.receiver, p.amount)
		} else {
			tr.nfts = append(tr.nfts, nftMove{token: p.token, serial: p.serial, from: p.sender, to: p.receiver})
		}
		tr.associate[relKey{account: p.receiver, token: p.token}] = true
	}
	if c := l.applyTransfer(t, tr); c != codeOK {
		return c
	}
	for _, p := range pending {
		delete(l.st.airdrops, p.airdropKey)
	}
	return codeOK
}

func (l *ledger) cancelAirdrops(ids []*services.PendingAirdropId) code {
	pending, c := l.lookupPending(ids)
	if c != codeOK {
		return c
	}
	for _, p := range pending {
		delete(l.st.airdrops, p.airdropKey)
	}
	return codeOK
}

func rejectOwner(t *txn, b *services.TokenRejectTransactionBody) *services.AccountID {
	if b.Owner != nil {
		return b.Owner
	}
	return accountProto(t.payer)
}

// reject hands tokens back to the treasury without charging custom fees.
func (l *ledger) reject(t *txn, b *services.TokenRejectTransactionBody) code {
	owner, c := l.st.liveAccount(rejectOwner(t, b))
	if c != codeOK {
		return c
	}
	switch {
	case len(b.Rejections) == 0:
		return services.ResponseCodeEnum_EMPTY_TOKEN_REFERENCE_LIST
	case len(b.Rejections) > maxPendingAirdropIDs:
		return services.ResponseCodeEnum_TOKEN_REFERENCE_LIST_SIZE_LIMIT_EXCEEDED
	}

	tr := newTransfer()
	for _, ref := range b.Rejections {
		switch r := ref.TokenIdentifier.(type) {
		case *services.TokenReference_FungibleToken:
			tok, c := l.st.token(r.FungibleToken)
			if c != codeOK {
				return c
			}
			rel := owner.tokens[tok.num]
			switch {
			case rel == nil:
				return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
			case tok.treasury == owner.num:
				return services.ResponseCodeEnum_ACCOUNT_IS_TREASURY
			case rel.balance <= 0:
				return services.ResponseCodeEnum_INSUFFICIENT_TOKEN_BALANCE
			}
			tr.addToken(tok.num, owner.num, -rel.balance)
			tr.addToken(tok.num, tok.treasury, rel.balance)
		case *services.TokenReference_Nft:
			tok, c := l.st.token(r.Nft.GetToken_ID())
			if c != codeOK {
				return c
			}
			item := tok.nfts[r.Nft.GetSerialNumber()]
			switch {
			case item == nil:
				return services.ResponseCodeEnum_INVALID_NFT_ID
			case item.owner != owner.num:
				return services.ResponseCodeEnum_INVALID_OWNER_ID
			case tok.treasury == owner.num:
				return services.ResponseCodeEnum_ACCOUNT_IS_TREASURY
			}
			tr.nfts = append(tr.nfts, nftMove{token: tok.num, serial: item.serial, from: owner.num, to: tok.treasury})
		default:
			return services.ResponseCodeEnum_INVALID_TRANSACTION_BODY
		}
	}
	return l.applyTransfer(t, tr)
}
