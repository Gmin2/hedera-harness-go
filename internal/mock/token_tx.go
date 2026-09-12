package mock

import (
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

const (
	maxCustomFees        = 10
	maxMetadataBytes     = 100
	maxTokenNameOrSymbol = 100
)

type tokenKeyKind int

const (
	supplyKey tokenKeyKind = iota
	kycKey
	freezeKey
	pauseKey
)

// tokenKey requires one of the token keys and reports the matching
// TOKEN_HAS_NO_*_KEY code when the token was created without it.
func (l *ledger) tokenKey(id *services.TokenID, kind tokenKeyKind) ([]*services.Key, code) {
	tok, c := l.st.token(id)
	if c != codeOK {
		return nil, c
	}
	var k *services.Key
	var missing code
	switch kind {
	case supplyKey:
		k, missing = tok.spec.SupplyKey, services.ResponseCodeEnum_TOKEN_HAS_NO_SUPPLY_KEY
	case kycKey:
		k, missing = tok.spec.KycKey, services.ResponseCodeEnum_TOKEN_HAS_NO_KYC_KEY
	case freezeKey:
		k, missing = tok.spec.FreezeKey, services.ResponseCodeEnum_TOKEN_HAS_NO_FREEZE_KEY
	case pauseKey:
		k, missing = tok.spec.PauseKey, services.ResponseCodeEnum_TOKEN_HAS_NO_PAUSE_KEY
	}
	if k == nil {
		return nil, missing
	}
	return []*services.Key{k}, codeOK
}

func (l *ledger) createTokenKeys(b *services.TokenCreateTransactionBody) ([]*services.Key, code) {
	treasury, c := l.st.account(b.Treasury)
	if c != codeOK {
		return nil, services.ResponseCodeEnum_INVALID_TREASURY_ACCOUNT_FOR_TOKEN
	}
	keys := []*services.Key{treasury.key, b.AdminKey}
	if b.AutoRenewAccount != nil {
		renew, c := l.st.account(b.AutoRenewAccount)
		if c != codeOK {
			return nil, services.ResponseCodeEnum_INVALID_AUTORENEW_ACCOUNT
		}
		keys = append(keys, renew.key)
	}
	return keys, codeOK
}

func (l *ledger) createToken(t *txn, b *services.TokenCreateTransactionBody) code {
	treasury, c := l.st.liveAccount(b.Treasury)
	if c != codeOK {
		return services.ResponseCodeEnum_INVALID_TREASURY_ACCOUNT_FOR_TOKEN
	}
	if c := validateTokenSpec(b); c != codeOK {
		return c
	}
	if c := l.validateCustomFees(b); c != codeOK {
		return c
	}

	tok := &token{
		num:      l.newEntityNum(),
		spec:     b,
		supply:   int64(b.InitialSupply),
		treasury: treasury.num,
		nfts:     map[int64]*nft{},
		created:  t.rec.consensus,
	}
	tok.fees = l.bindFees(tok.num, b.CustomFees)
	l.st.tokens[tok.num] = tok

	// the treasury and the collectors of fees paid in this token are
	// associated automatically, kyc granted and unfrozen
	associated := []int64{treasury.num}
	for _, fee := range tok.fees {
		collector := fee.FeeCollectorAccountId.GetAccountNum()
		paidInToken := false
		switch f := fee.Fee.(type) {
		case *services.CustomFee_FractionalFee:
			paidInToken = true
		case *services.CustomFee_FixedFee:
			paidInToken = f.FixedFee.GetDenominatingTokenId().GetTokenNum() == tok.num
		}
		if paidInToken && collector != treasury.num {
			associated = append(associated, collector)
		}
	}
	for _, num := range associated {
		a := l.st.accounts[num]
		if a.tokens[tok.num] != nil {
			continue
		}
		rel := newRelation(tok, false, t.rec.consensus)
		if b.KycKey != nil {
			rel.kyc = services.TokenKycStatus_Granted
		}
		if b.FreezeKey != nil {
			rel.freeze = services.TokenFreezeStatus_Unfrozen
		}
		a.tokens[tok.num] = rel
		t.rec.autoAssociations = append(t.rec.autoAssociations, &services.TokenAssociation{
			TokenId:   tokenProto(tok.num),
			AccountId: accountProto(num),
		})
	}
	treasury.tokens[tok.num].balance = tok.supply

	t.rec.receipt.TokenID = tokenProto(tok.num)
	t.rec.entity = tok.num
	if tok.supply > 0 {
		tr := newTransfer()
		tr.addToken(tok.num, treasury.num, tok.supply)
		t.rec.tokenTransfers = tokenTransferLists(tr)
	}
	return codeOK
}

func validateTokenSpec(b *services.TokenCreateTransactionBody) code {
	switch {
	case b.Name == "":
		return services.ResponseCodeEnum_MISSING_TOKEN_NAME
	case len(b.Name) > maxTokenNameOrSymbol:
		return services.ResponseCodeEnum_TOKEN_NAME_TOO_LONG
	case b.Symbol == "":
		return services.ResponseCodeEnum_MISSING_TOKEN_SYMBOL
	case len(b.Symbol) > maxTokenNameOrSymbol:
		return services.ResponseCodeEnum_TOKEN_SYMBOL_TOO_LONG
	case len(b.Memo) > maxMemoBytes:
		return services.ResponseCodeEnum_MEMO_TOO_LONG
	case int64(b.InitialSupply) < 0:
		return services.ResponseCodeEnum_INVALID_TOKEN_INITIAL_SUPPLY
	case b.FreezeDefault && b.FreezeKey == nil:
		return services.ResponseCodeEnum_TOKEN_HAS_NO_FREEZE_KEY
	case len(b.CustomFees) > maxCustomFees:
		return services.ResponseCodeEnum_CUSTOM_FEES_LIST_TOO_LONG
	}
	for _, k := range []*services.Key{b.AdminKey, b.KycKey, b.FreezeKey, b.WipeKey, b.SupplyKey, b.FeeScheduleKey, b.PauseKey, b.MetadataKey} {
		if k != nil && !validKey(k) {
			return services.ResponseCodeEnum_BAD_ENCODING
		}
	}

	if b.TokenType == services.TokenType_NON_FUNGIBLE_UNIQUE {
		switch {
		case b.InitialSupply != 0:
			return services.ResponseCodeEnum_INVALID_TOKEN_INITIAL_SUPPLY
		case b.Decimals != 0:
			return services.ResponseCodeEnum_INVALID_TOKEN_DECIMALS
		case b.SupplyKey == nil:
			return services.ResponseCodeEnum_TOKEN_HAS_NO_SUPPLY_KEY
		}
	}

	switch b.SupplyType {
	case services.TokenSupplyType_FINITE:
		if b.MaxSupply <= 0 {
			return services.ResponseCodeEnum_INVALID_TOKEN_MAX_SUPPLY
		}
		if int64(b.InitialSupply) > b.MaxSupply {
			return services.ResponseCodeEnum_INVALID_TOKEN_INITIAL_SUPPLY
		}
	case services.TokenSupplyType_INFINITE:
		if b.MaxSupply != 0 {
			return services.ResponseCodeEnum_INVALID_TOKEN_MAX_SUPPLY
		}
	}
	return codeOK
}

func (l *ledger) validateCustomFees(b *services.TokenCreateTransactionBody) code {
	fungible := b.TokenType == services.TokenType_FUNGIBLE_COMMON
	checkFixed := func(f *services.FixedFee, collector int64) code {
		if f.GetAmount() <= 0 {
			return services.ResponseCodeEnum_CUSTOM_FEE_MUST_BE_POSITIVE
		}
		denom := f.GetDenominatingTokenId()
		if denom == nil || denom.TokenNum == 0 {
			if denom != nil && !fungible {
				return services.ResponseCodeEnum_CUSTOM_FEE_DENOMINATION_MUST_BE_FUNGIBLE_COMMON
			}
			return codeOK
		}
		feeToken, c := l.st.token(denom)
		if c != codeOK || !feeToken.fungible() {
			return services.ResponseCodeEnum_INVALID_TOKEN_ID_IN_CUSTOM_FEES
		}
		if l.st.accounts[collector].tokens[feeToken.num] == nil {
			return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_FEE_COLLECTOR
		}
		return codeOK
	}

	for _, fee := range b.CustomFees {
		collector, c := l.st.liveAccount(fee.FeeCollectorAccountId)
		if c != codeOK {
			return services.ResponseCodeEnum_INVALID_CUSTOM_FEE_COLLECTOR
		}
		switch f := fee.Fee.(type) {
		case *services.CustomFee_FixedFee:
			if c := checkFixed(f.FixedFee, collector.num); c != codeOK {
				return c
			}
		case *services.CustomFee_FractionalFee:
			if !fungible {
				return services.ResponseCodeEnum_CUSTOM_FRACTIONAL_FEE_ONLY_ALLOWED_FOR_FUNGIBLE_COMMON
			}
			frac := f.FractionalFee.GetFractionalAmount()
			if frac.GetDenominator() == 0 {
				return services.ResponseCodeEnum_FRACTION_DIVIDES_BY_ZERO
			}
			if frac.GetNumerator() <= 0 || f.FractionalFee.MinimumAmount < 0 || f.FractionalFee.MaximumAmount < 0 {
				return services.ResponseCodeEnum_CUSTOM_FEE_MUST_BE_POSITIVE
			}
			if most := f.FractionalFee.MaximumAmount; most > 0 && f.FractionalFee.MinimumAmount > most {
				return services.ResponseCodeEnum_FRACTIONAL_FEE_MAX_AMOUNT_LESS_THAN_MIN_AMOUNT
			}
		case *services.CustomFee_RoyaltyFee:
			if fungible {
				return services.ResponseCodeEnum_CUSTOM_ROYALTY_FEE_ONLY_ALLOWED_FOR_NON_FUNGIBLE_UNIQUE
			}
			frac := f.RoyaltyFee.GetExchangeValueFraction()
			if frac.GetDenominator() == 0 {
				return services.ResponseCodeEnum_FRACTION_DIVIDES_BY_ZERO
			}
			if frac.GetNumerator() > frac.GetDenominator() {
				return services.ResponseCodeEnum_ROYALTY_FRACTION_CANNOT_EXCEED_ONE
			}
			if fb := f.RoyaltyFee.FallbackFee; fb != nil {
				if c := checkFixed(fb, collector.num); c != codeOK {
					return c
				}
			}
		default:
			return services.ResponseCodeEnum_CUSTOM_FEE_NOT_FULLY_SPECIFIED
		}
	}
	return codeOK
}

// bindFees rewrites fixed fees denominated in 0.0.0 to the token being created.
func (l *ledger) bindFees(tokNum int64, fees []*services.CustomFee) []*services.CustomFee {
	out := make([]*services.CustomFee, 0, len(fees))
	for _, fee := range fees {
		f, isFixed := fee.Fee.(*services.CustomFee_FixedFee)
		if !isFixed || f.FixedFee.DenominatingTokenId == nil || f.FixedFee.DenominatingTokenId.TokenNum != 0 {
			out = append(out, fee)
			continue
		}
		out = append(out, &services.CustomFee{
			Fee: &services.CustomFee_FixedFee{FixedFee: &services.FixedFee{
				Amount:              f.FixedFee.Amount,
				DenominatingTokenId: tokenProto(tokNum),
			}},
			FeeCollectorAccountId:  fee.FeeCollectorAccountId,
			AllCollectorsAreExempt: fee.AllCollectorsAreExempt,
		})
	}
	return out
}

func (l *ledger) associate(t *txn, b *services.TokenAssociateTransactionBody) code {
	a, c := l.st.liveAccount(b.Account)
	if c != codeOK {
		return c
	}
	var toks []*token
	seen := map[int64]bool{}
	for _, id := range b.Tokens {
		tok, c := l.st.token(id)
		if c != codeOK {
			return c
		}
		switch {
		case seen[tok.num]:
			return services.ResponseCodeEnum_TOKEN_ID_REPEATED_IN_TOKEN_LIST
		case tok.paused:
			return services.ResponseCodeEnum_TOKEN_IS_PAUSED
		case a.tokens[tok.num] != nil:
			return services.ResponseCodeEnum_TOKEN_ALREADY_ASSOCIATED_TO_ACCOUNT
		}
		seen[tok.num] = true
		toks = append(toks, tok)
	}
	for _, tok := range toks {
		a.tokens[tok.num] = newRelation(tok, false, t.rec.consensus)
	}
	t.rec.entity = a.num
	return codeOK
}

func (l *ledger) dissociate(t *txn, b *services.TokenDissociateTransactionBody) code {
	a, c := l.st.liveAccount(b.Account)
	if c != codeOK {
		return c
	}
	var nums []int64
	for _, id := range b.Tokens {
		tok, c := l.st.token(id)
		if c != codeOK {
			return c
		}
		rel := a.tokens[tok.num]
		switch {
		case rel == nil:
			return services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
		case tok.treasury == a.num:
			return services.ResponseCodeEnum_ACCOUNT_IS_TREASURY
		case tok.paused:
			return services.ResponseCodeEnum_TOKEN_IS_PAUSED
		case rel.freeze == services.TokenFreezeStatus_Frozen:
			return services.ResponseCodeEnum_ACCOUNT_FROZEN_FOR_TOKEN
		case rel.balance != 0:
			return services.ResponseCodeEnum_TRANSACTION_REQUIRES_ZERO_TOKEN_BALANCES
		}
		nums = append(nums, tok.num)
	}
	for _, num := range nums {
		if a.tokens[num].automatic {
			a.usedAutoAssociations--
		}
		delete(a.tokens, num)
	}
	t.rec.entity = a.num
	return codeOK
}

func (l *ledger) mint(t *txn, b *services.TokenMintTransactionBody) code {
	tok, c := l.st.token(b.Token)
	if c != codeOK {
		return c
	}
	if tok.paused {
		return services.ResponseCodeEnum_TOKEN_IS_PAUSED
	}
	treasury := l.st.accounts[tok.treasury]
	finite := tok.spec.SupplyType == services.TokenSupplyType_FINITE
	t.rec.entity = tok.num

	if tok.fungible() {
		amount := int64(b.Amount)
		if amount <= 0 || len(b.Metadata) > 0 {
			return services.ResponseCodeEnum_INVALID_TOKEN_MINT_AMOUNT
		}
		if finite && tok.supply+amount > tok.spec.MaxSupply {
			return services.ResponseCodeEnum_TOKEN_MAX_SUPPLY_REACHED
		}
		tok.supply += amount
		treasury.tokens[tok.num].balance += amount

		tr := newTransfer()
		tr.addToken(tok.num, treasury.num, amount)
		t.rec.tokenTransfers = tokenTransferLists(tr)
		t.rec.receipt.NewTotalSupply = uint64(tok.supply)
		return codeOK
	}

	if len(b.Metadata) == 0 || b.Amount != 0 {
		return services.ResponseCodeEnum_INVALID_TOKEN_MINT_METADATA
	}
	for _, md := range b.Metadata {
		if len(md) > maxMetadataBytes {
			return services.ResponseCodeEnum_METADATA_TOO_LONG
		}
	}
	count := int64(len(b.Metadata))
	if finite && tok.supply+count > tok.spec.MaxSupply {
		return services.ResponseCodeEnum_TOKEN_MAX_SUPPLY_REACHED
	}

	list := &services.TokenTransferList{Token: tokenProto(tok.num)}
	for _, md := range b.Metadata {
		tok.lastSerial++
		tok.nfts[tok.lastSerial] = &nft{
			serial:   tok.lastSerial,
			owner:    treasury.num,
			metadata: md,
			created:  t.rec.consensus,
			modified: t.rec.consensus,
		}
		t.rec.receipt.SerialNumbers = append(t.rec.receipt.SerialNumbers, tok.lastSerial)
		list.NftTransfers = append(list.NftTransfers, &services.NftTransfer{
			SenderAccountID:   accountProto(0),
			ReceiverAccountID: accountProto(treasury.num),
			SerialNumber:      tok.lastSerial,
		})
	}
	tok.supply += count
	treasury.tokens[tok.num].balance += count
	t.rec.tokenTransfers = []*services.TokenTransferList{list}
	t.rec.receipt.NewTotalSupply = uint64(tok.supply)
	return codeOK
}

func (l *ledger) burn(t *txn, b *services.TokenBurnTransactionBody) code {
	tok, c := l.st.token(b.Token)
	if c != codeOK {
		return c
	}
	if tok.paused {
		return services.ResponseCodeEnum_TOKEN_IS_PAUSED
	}
	treasury := l.st.accounts[tok.treasury]
	rel := treasury.tokens[tok.num]
	t.rec.entity = tok.num

	if tok.fungible() {
		amount := int64(b.Amount)
		if amount <= 0 || len(b.SerialNumbers) > 0 {
			return services.ResponseCodeEnum_INVALID_TOKEN_BURN_AMOUNT
		}
		if rel.balance < amount {
			return services.ResponseCodeEnum_INSUFFICIENT_TOKEN_BALANCE
		}
		tok.supply -= amount
		rel.balance -= amount

		tr := newTransfer()
		tr.addToken(tok.num, treasury.num, -amount)
		t.rec.tokenTransfers = tokenTransferLists(tr)
		t.rec.receipt.NewTotalSupply = uint64(tok.supply)
		return codeOK
	}

	if len(b.SerialNumbers) == 0 || b.Amount != 0 {
		return services.ResponseCodeEnum_INVALID_TOKEN_BURN_METADATA
	}
	seen := map[int64]bool{}
	for _, serial := range b.SerialNumbers {
		n := tok.nfts[serial]
		switch {
		case n == nil || seen[serial]:
			return services.ResponseCodeEnum_INVALID_NFT_ID
		case n.owner != treasury.num:
			return services.ResponseCodeEnum_TREASURY_MUST_OWN_BURNED_NFT
		}
		seen[serial] = true
	}
	list := &services.TokenTransferList{Token: tokenProto(tok.num)}
	for _, serial := range b.SerialNumbers {
		delete(tok.nfts, serial)
		list.NftTransfers = append(list.NftTransfers, &services.NftTransfer{
			SenderAccountID:   accountProto(treasury.num),
			ReceiverAccountID: accountProto(0),
			SerialNumber:      serial,
		})
	}
	tok.supply -= int64(len(b.SerialNumbers))
	rel.balance -= int64(len(b.SerialNumbers))
	t.rec.tokenTransfers = []*services.TokenTransferList{list}
	t.rec.receipt.NewTotalSupply = uint64(tok.supply)
	return codeOK
}

// relation finds the relationship a kyc or freeze change applies to.
func (l *ledger) relation(tokenID *services.TokenID, accountID *services.AccountID) (*tokenRelation, code) {
	tok, c := l.st.token(tokenID)
	if c != codeOK {
		return nil, c
	}
	a, c := l.st.liveAccount(accountID)
	if c != codeOK {
		return nil, c
	}
	if tok.paused {
		return nil, services.ResponseCodeEnum_TOKEN_IS_PAUSED
	}
	rel := a.tokens[tok.num]
	if rel == nil {
		return nil, services.ResponseCodeEnum_TOKEN_NOT_ASSOCIATED_TO_ACCOUNT
	}
	return rel, codeOK
}

func (l *ledger) setKyc(t *txn, tokenID *services.TokenID, accountID *services.AccountID, status services.TokenKycStatus) code {
	rel, c := l.relation(tokenID, accountID)
	if c != codeOK {
		return c
	}
	rel.kyc = status
	t.rec.entity = accountID.GetAccountNum()
	return codeOK
}

func (l *ledger) setFreeze(t *txn, tokenID *services.TokenID, accountID *services.AccountID, status services.TokenFreezeStatus) code {
	rel, c := l.relation(tokenID, accountID)
	if c != codeOK {
		return c
	}
	rel.freeze = status
	t.rec.entity = accountID.GetAccountNum()
	return codeOK
}

func (l *ledger) setPaused(t *txn, tokenID *services.TokenID, paused bool) code {
	tok, c := l.st.token(tokenID)
	if c != codeOK {
		return c
	}
	tok.paused = paused
	t.rec.entity = tok.num
	return codeOK
}
