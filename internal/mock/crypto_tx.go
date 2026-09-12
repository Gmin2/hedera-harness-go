package mock

import (
	"encoding/hex"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

// accountKey is the key requirement for transactions signed by one account.
func (l *ledger) accountKey(id *services.AccountID) ([]*services.Key, code) {
	a, c := l.st.account(id)
	if c != codeOK {
		return nil, c
	}
	return []*services.Key{a.key}, codeOK
}

func (l *ledger) createAccountKeys(b *services.CryptoCreateTransactionBody) ([]*services.Key, code) {
	if b.ReceiverSigRequired {
		return []*services.Key{b.Key}, codeOK
	}
	return nil, codeOK
}

func (l *ledger) createAccount(t *txn, b *services.CryptoCreateTransactionBody) code {
	if b.Key == nil {
		return services.ResponseCodeEnum_KEY_REQUIRED
	}
	if !validKey(b.Key) {
		return services.ResponseCodeEnum_BAD_ENCODING
	}
	if len(b.Memo) > maxMemoBytes {
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	}
	if b.MaxAutomaticTokenAssociations < -1 {
		return services.ResponseCodeEnum_INVALID_MAX_AUTO_ASSOCIATIONS
	}

	var alias string
	if len(b.Alias) > 0 {
		if len(b.Alias) != 20 {
			return services.ResponseCodeEnum_INVALID_ALIAS_KEY
		}
		alias = hex.EncodeToString(b.Alias)
		if _, taken := l.st.aliases[alias]; taken {
			return services.ResponseCodeEnum_ALIAS_ALREADY_ASSIGNED
		}
		// the ecdsa key the evm address was derived from has to prove it
		if !t.sigs.signedByEvm(alias) {
			return services.ResponseCodeEnum_INVALID_SIGNATURE
		}
	}

	payer := l.st.accounts[t.payer]
	amount := int64(b.InitialBalance)
	if amount < 0 {
		return services.ResponseCodeEnum_INVALID_INITIAL_BALANCE
	}
	if payer.balance < amount {
		return services.ResponseCodeEnum_INSUFFICIENT_PAYER_BALANCE
	}

	a := &account{
		num:                 l.newEntityNum(),
		key:                 b.Key,
		balance:             amount,
		memo:                b.Memo,
		maxAutoAssociations: b.MaxAutomaticTokenAssociations,
		receiverSigRequired: b.ReceiverSigRequired,
		created:             t.rec.consensus,
		tokens:              map[int64]*tokenRelation{},
	}
	payer.balance -= amount
	l.st.accounts[a.num] = a
	if alias != "" {
		a.alias = b.Alias
		l.st.aliases[alias] = a.num
	}

	t.rec.receipt.AccountID = accountProto(a.num)
	t.rec.entity = a.num
	t.rec.transfers = hbarTransfers(map[int64]int64{payer.num: -amount, a.num: amount})
	return codeOK
}

func (l *ledger) updateAccountKeys(b *services.CryptoUpdateTransactionBody) ([]*services.Key, code) {
	keys, c := l.accountKey(b.AccountIDToUpdate)
	if c != codeOK {
		return nil, c
	}
	if b.Key != nil {
		keys = append(keys, b.Key)
	}
	return keys, codeOK
}

func (l *ledger) updateAccount(b *services.CryptoUpdateTransactionBody) code {
	a, c := l.st.liveAccount(b.AccountIDToUpdate)
	if c != codeOK {
		return c
	}
	if b.Key != nil && !validKey(b.Key) {
		return services.ResponseCodeEnum_BAD_ENCODING
	}
	if b.Memo != nil && len(b.Memo.Value) > maxMemoBytes {
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	}
	if m := b.MaxAutomaticTokenAssociations; m != nil {
		if m.Value < -1 {
			return services.ResponseCodeEnum_INVALID_MAX_AUTO_ASSOCIATIONS
		}
		if m.Value != -1 && m.Value < a.usedAutoAssociations {
			return services.ResponseCodeEnum_EXISTING_AUTOMATIC_ASSOCIATIONS_EXCEED_GIVEN_LIMIT
		}
	}

	if b.Key != nil {
		a.key = b.Key
	}
	if b.Memo != nil {
		a.memo = b.Memo.Value
	}
	if m := b.MaxAutomaticTokenAssociations; m != nil {
		a.maxAutoAssociations = m.Value
	}
	switch v := b.ReceiverSigRequiredField.(type) {
	case *services.CryptoUpdateTransactionBody_ReceiverSigRequired:
		a.receiverSigRequired = v.ReceiverSigRequired
	case *services.CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper:
		a.receiverSigRequired = v.ReceiverSigRequiredWrapper.GetValue()
	}
	return codeOK
}

func (l *ledger) deleteAccountKeys(b *services.CryptoDeleteTransactionBody) ([]*services.Key, code) {
	keys, c := l.accountKey(b.DeleteAccountID)
	if c != codeOK {
		return nil, c
	}
	if to, c := l.st.account(b.TransferAccountID); c == codeOK && to.receiverSigRequired {
		keys = append(keys, to.key)
	}
	return keys, codeOK
}

func (l *ledger) deleteAccount(t *txn, b *services.CryptoDeleteTransactionBody) code {
	a, c := l.st.liveAccount(b.DeleteAccountID)
	if c != codeOK {
		return c
	}
	to, c := l.st.liveAccount(b.TransferAccountID)
	if c != codeOK {
		return services.ResponseCodeEnum_INVALID_TRANSFER_ACCOUNT_ID
	}
	if a.num == to.num {
		return services.ResponseCodeEnum_TRANSFER_ACCOUNT_SAME_AS_DELETE_ACCOUNT
	}
	for num, rel := range a.tokens {
		if l.st.tokens[num].treasury == a.num {
			return services.ResponseCodeEnum_ACCOUNT_IS_TREASURY
		}
		if rel.balance != 0 {
			return services.ResponseCodeEnum_TRANSACTION_REQUIRES_ZERO_TOKEN_BALANCES
		}
	}

	amount := a.balance
	to.balance += amount
	a.balance = 0
	a.deleted = true
	t.rec.entity = a.num
	t.rec.transfers = hbarTransfers(map[int64]int64{a.num: -amount, to.num: amount})
	return codeOK
}

// transferKeys requires every debited account, every nft sender and any
// receiver that asked for receiver signatures.
func (l *ledger) transferKeys(b *services.CryptoTransferTransactionBody) ([]*services.Key, code) {
	var keys []*services.Key
	add := func(id *services.AccountID, sending bool) code {
		a, c := l.st.account(id)
		if c != codeOK {
			return c
		}
		if sending || a.receiverSigRequired {
			keys = append(keys, a.key)
		}
		return codeOK
	}
	for _, aa := range b.GetTransfers().GetAccountAmounts() {
		if c := add(aa.AccountID, aa.Amount < 0); c != codeOK {
			return nil, c
		}
	}
	for _, list := range b.GetTokenTransfers() {
		for _, aa := range list.Transfers {
			if c := add(aa.AccountID, aa.Amount < 0); c != codeOK {
				return nil, c
			}
		}
		for _, n := range list.NftTransfers {
			if c := add(n.SenderAccountID, true); c != codeOK {
				return nil, c
			}
			if c := add(n.ReceiverAccountID, false); c != codeOK {
				return nil, c
			}
		}
	}
	return keys, codeOK
}

func (l *ledger) cryptoTransfer(t *txn, b *services.CryptoTransferTransactionBody) code {
	tr := newTransfer()
	var hbarSum int64
	for _, aa := range b.GetTransfers().GetAccountAmounts() {
		a, c := l.st.account(aa.AccountID)
		if c != codeOK {
			return c
		}
		if aa.IsApproval {
			return services.ResponseCodeEnum_NOT_SUPPORTED
		}
		tr.hbar[a.num] += aa.Amount
		hbarSum += aa.Amount
	}
	if hbarSum != 0 {
		return services.ResponseCodeEnum_INVALID_ACCOUNT_AMOUNTS
	}

	seen := map[int64]bool{}
	for _, list := range b.GetTokenTransfers() {
		tok, c := l.st.token(list.Token)
		if c != codeOK {
			return c
		}
		if seen[tok.num] {
			return services.ResponseCodeEnum_TOKEN_ID_REPEATED_IN_TOKEN_LIST
		}
		seen[tok.num] = true
		if len(list.Transfers) > 0 && !tok.fungible() {
			return services.ResponseCodeEnum_ACCOUNT_AMOUNT_TRANSFERS_ONLY_ALLOWED_FOR_FUNGIBLE_COMMON
		}
		if len(list.NftTransfers) > 0 && tok.fungible() {
			return services.ResponseCodeEnum_INVALID_NFT_ID
		}
		if list.ExpectedDecimals != nil && list.ExpectedDecimals.Value != tok.spec.Decimals {
			return services.ResponseCodeEnum_UNEXPECTED_TOKEN_DECIMALS
		}

		var sum int64
		for _, aa := range list.Transfers {
			a, c := l.st.account(aa.AccountID)
			if c != codeOK {
				return c
			}
			if aa.IsApproval {
				return services.ResponseCodeEnum_NOT_SUPPORTED
			}
			tr.addToken(tok.num, a.num, aa.Amount)
			sum += aa.Amount
		}
		if sum != 0 {
			return services.ResponseCodeEnum_TRANSFERS_NOT_ZERO_SUM_FOR_TOKEN
		}
		for _, n := range list.NftTransfers {
			from, c := l.st.account(n.SenderAccountID)
			if c != codeOK {
				return c
			}
			to, c := l.st.account(n.ReceiverAccountID)
			if c != codeOK {
				return c
			}
			if n.IsApproval {
				return services.ResponseCodeEnum_NOT_SUPPORTED
			}
			tr.nfts = append(tr.nfts, nftMove{token: tok.num, serial: n.SerialNumber, from: from.num, to: to.num})
		}
	}
	if len(tr.hbar) == 0 && len(tr.tokens) == 0 && len(tr.nfts) == 0 {
		return services.ResponseCodeEnum_EMPTY_TOKEN_TRANSFER_BODY
	}

	if c := l.assessCustomFees(tr); c != codeOK {
		return c
	}
	return l.applyTransfer(t, tr)
}
