package mock

import (
	"maps"
	"math/big"
	"slices"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

// assessCustomFees adds the custom fee legs of every token in tr, based on
// the transfers as the user wrote them. Nested fees (a fee paid in a token
// that has fees of its own) are not charged.
func (l *ledger) assessCustomFees(tr *transfer) code {
	origHbar := maps.Clone(tr.hbar)
	origTokens := map[int64]map[int64]int64{}
	for tok, deltas := range tr.tokens {
		origTokens[tok] = maps.Clone(deltas)
	}

	for _, tokNum := range slices.Sorted(maps.Keys(origTokens)) {
		tok := l.st.tokens[tokNum]
		if tok == nil {
			continue // applyTransfer reports the bad token id
		}
		deltas := origTokens[tokNum]
		senders, receivers := splitDeltas(deltas)
		for _, fee := range tok.fees {
			collector := fee.FeeCollectorAccountId.GetAccountNum()
			switch f := fee.Fee.(type) {
			case *services.CustomFee_FixedFee:
				for _, sender := range senders {
					if !feeExempt(tok, fee, sender) {
						tr.chargeFixed(f.FixedFee, sender, collector)
					}
				}
			case *services.CustomFee_FractionalFee:
				for _, sender := range senders {
					if feeExempt(tok, fee, sender) {
						continue
					}
					if c := tr.chargeFractional(tokNum, f.FractionalFee, -deltas[sender], sender, collector, receivers, deltas); c != codeOK {
						return c
					}
				}
			}
		}
	}

	charged := map[[3]int64]bool{}
	for _, m := range tr.nfts {
		tok := l.st.tokens[m.token]
		if tok == nil {
			continue
		}
		for i, fee := range tok.fees {
			royalty, isRoyalty := fee.Fee.(*services.CustomFee_RoyaltyFee)
			if !isRoyalty || feeExempt(tok, fee, m.from) {
				continue
			}
			once := [3]int64{m.token, m.from, int64(i)}
			if charged[once] {
				continue
			}
			charged[once] = true
			tr.chargeRoyalty(royalty.RoyaltyFee, m, fee.FeeCollectorAccountId.GetAccountNum(), origHbar, origTokens)
		}
	}
	return codeOK
}

// feeExempt applies the HIP-573 rules: the treasury never pays, a collector
// never pays its own fee, and with all_collectors_are_exempt no collector of
// the token pays any of its fees.
func feeExempt(tok *token, fee *services.CustomFee, payer int64) bool {
	if payer == tok.treasury || payer == fee.FeeCollectorAccountId.GetAccountNum() {
		return true
	}
	if !fee.AllCollectorsAreExempt {
		return false
	}
	for _, other := range tok.fees {
		if other.FeeCollectorAccountId.GetAccountNum() == payer {
			return true
		}
	}
	return false
}

func splitDeltas(deltas map[int64]int64) (senders, receivers []int64) {
	for _, acct := range slices.Sorted(maps.Keys(deltas)) {
		switch {
		case deltas[acct] < 0:
			senders = append(senders, acct)
		case deltas[acct] > 0:
			receivers = append(receivers, acct)
		}
	}
	return senders, receivers
}

func (tr *transfer) chargeFixed(f *services.FixedFee, payer, collector int64) {
	assessed := &services.AssessedCustomFee{
		Amount:                  f.Amount,
		FeeCollectorAccountId:   accountProto(collector),
		EffectivePayerAccountId: []*services.AccountID{accountProto(payer)},
	}
	if f.DenominatingTokenId == nil {
		tr.hbar[payer] -= f.Amount
		tr.hbar[collector] += f.Amount
	} else {
		denom := f.DenominatingTokenId.TokenNum
		tr.addToken(denom, payer, -f.Amount)
		tr.addToken(denom, collector, f.Amount)
		assessed.TokenId = tokenProto(denom)
	}
	tr.fees = append(tr.fees, assessed)
}

// chargeFractional takes a share of what sender sent. By default the share
// comes out of the receivers credits, with net_of_transfers the sender pays
// it on top.
func (tr *transfer) chargeFractional(tok int64, f *services.FractionalFee, sent, sender, collector int64, receivers []int64, deltas map[int64]int64) code {
	frac := f.GetFractionalAmount()
	if frac.GetDenominator() == 0 {
		return services.ResponseCodeEnum_FRACTION_DIVIDES_BY_ZERO
	}
	amount := mulDiv(sent, frac.Numerator, frac.Denominator)
	if amount < f.MinimumAmount {
		amount = f.MinimumAmount
	}
	if f.MaximumAmount > 0 && amount > f.MaximumAmount {
		amount = f.MaximumAmount
	}
	if amount <= 0 {
		return codeOK
	}

	assessed := &services.AssessedCustomFee{
		Amount:                amount,
		TokenId:               tokenProto(tok),
		FeeCollectorAccountId: accountProto(collector),
	}
	if f.NetOfTransfers {
		tr.addToken(tok, sender, -amount)
		assessed.EffectivePayerAccountId = []*services.AccountID{accountProto(sender)}
	} else {
		var credited int64
		for _, r := range receivers {
			credited += deltas[r]
		}
		if credited < amount {
			return services.ResponseCodeEnum_INSUFFICIENT_SENDER_ACCOUNT_BALANCE_FOR_CUSTOM_FEE
		}
		left := amount
		for i, r := range receivers {
			share := mulDiv(amount, deltas[r], credited)
			if i == len(receivers)-1 {
				share = left
			}
			if share == 0 {
				continue
			}
			tr.addToken(tok, r, -share)
			left -= share
			assessed.EffectivePayerAccountId = append(assessed.EffectivePayerAccountId, accountProto(r))
		}
	}
	tr.addToken(tok, collector, amount)
	tr.fees = append(tr.fees, assessed)
	return codeOK
}

// chargeRoyalty takes a share of the fungible value the nft sender receives in
// the same transaction, or charges the fallback fee to the nft receiver when
// nothing of value moves back.
func (tr *transfer) chargeRoyalty(f *services.RoyaltyFee, m nftMove, collector int64, hbar map[int64]int64, tokens map[int64]map[int64]int64) {
	frac := f.GetExchangeValueFraction()
	paid := false
	if v := hbar[m.from]; v > 0 {
		paid = true
		if amount := mulDiv(v, frac.GetNumerator(), frac.GetDenominator()); amount > 0 {
			tr.chargeFixed(&services.FixedFee{Amount: amount}, m.from, collector)
		}
	}
	for _, tok := range slices.Sorted(maps.Keys(tokens)) {
		if v := tokens[tok][m.from]; v > 0 {
			paid = true
			if amount := mulDiv(v, frac.GetNumerator(), frac.GetDenominator()); amount > 0 {
				tr.chargeFixed(&services.FixedFee{Amount: amount, DenominatingTokenId: tokenProto(tok)}, m.from, collector)
			}
		}
	}
	if !paid && f.FallbackFee != nil {
		tr.chargeFixed(f.FallbackFee, m.to, collector)
	}
}

func mulDiv(a, num, den int64) int64 {
	if den == 0 {
		return 0
	}
	r := new(big.Int).Mul(big.NewInt(a), big.NewInt(num))
	return r.Quo(r, big.NewInt(den)).Int64()
}
