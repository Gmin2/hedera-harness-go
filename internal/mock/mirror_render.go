package mock

import (
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	"google.golang.org/protobuf/proto"
)

// These two list shapes are not part of internal/mirror yet.

type accountBalanceRow struct {
	Account string                `json:"account"`
	Balance int64                 `json:"balance"`
	Tokens  []mirror.TokenBalance `json:"tokens"`
}

type balancesResponse struct {
	Timestamp string              `json:"timestamp"`
	Balances  []accountBalanceRow `json:"balances"`
	Links     mirror.Links        `json:"links"`
}

type tokenBalanceRow struct {
	Account  string `json:"account"`
	Balance  int64  `json:"balance"`
	Decimals int64  `json:"decimals"`
}

type tokenBalancesResponse struct {
	Timestamp string            `json:"timestamp"`
	Balances  []tokenBalanceRow `json:"balances"`
	Links     mirror.Links      `json:"links"`
}

func stringPtr(s string) *string { return &s }

func renderAccount(a *account, at string) mirror.Account {
	out := mirror.Account{
		Account:    entityString(a.num),
		EvmAddress: evmAddress(a),
		Balance: mirror.AccountBalance{
			Balance:   a.balance,
			Timestamp: at,
			Tokens:    renderTokenBalances(a),
		},
		Key:                           mirrorKey(a.key),
		MaxAutomaticTokenAssociations: a.maxAutoAssociations,
		ReceiverSigRequired:           a.receiverSigRequired,
		Memo:                          a.memo,
		Deleted:                       a.deleted,
		CreatedTimestamp:              mirrorTime(a.created),
	}
	if len(a.alias) > 0 {
		out.Alias = stringPtr(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(a.alias))
	}
	return out
}

func renderTokenBalances(a *account) []mirror.TokenBalance {
	out := []mirror.TokenBalance{}
	for _, num := range slices.Sorted(maps.Keys(a.tokens)) {
		out = append(out, mirror.TokenBalance{TokenID: entityString(num), Balance: a.tokens[num].balance})
	}
	return out
}

var (
	kycNames = map[services.TokenKycStatus]string{
		services.TokenKycStatus_KycNotApplicable: "NOT_APPLICABLE",
		services.TokenKycStatus_Granted:          "GRANTED",
		services.TokenKycStatus_Revoked:          "REVOKED",
	}
	freezeNames = map[services.TokenFreezeStatus]string{
		services.TokenFreezeStatus_FreezeNotApplicable: "NOT_APPLICABLE",
		services.TokenFreezeStatus_Frozen:              "FROZEN",
		services.TokenFreezeStatus_Unfrozen:            "UNFROZEN",
	}
	pauseNames = map[services.TokenPauseStatus]string{
		services.TokenPauseStatus_PauseNotApplicable: "NOT_APPLICABLE",
		services.TokenPauseStatus_Paused:             "PAUSED",
		services.TokenPauseStatus_Unpaused:           "UNPAUSED",
	}
)

func renderRelationship(tok *token, rel *tokenRelation) mirror.TokenRelationship {
	return mirror.TokenRelationship{
		TokenID:              entityString(tok.num),
		Balance:              rel.balance,
		Decimals:             int64(tok.spec.Decimals),
		AutomaticAssociation: rel.automatic,
		FreezeStatus:         freezeNames[rel.freeze],
		KycStatus:            kycNames[rel.kyc],
		CreatedTimestamp:     mirrorTime(rel.created),
	}
}

func renderToken(tok *token) mirror.Token {
	s := tok.spec
	out := mirror.Token{
		TokenID:           entityString(tok.num),
		Type:              "FUNGIBLE_COMMON",
		Name:              s.Name,
		Symbol:            s.Symbol,
		Memo:              s.Memo,
		Decimals:          strconv.FormatUint(uint64(s.Decimals), 10),
		InitialSupply:     strconv.FormatUint(s.InitialSupply, 10),
		TotalSupply:       strconv.FormatInt(tok.supply, 10),
		MaxSupply:         strconv.FormatInt(s.MaxSupply, 10),
		SupplyType:        "INFINITE",
		TreasuryAccountID: entityString(tok.treasury),
		AdminKey:          mirrorKey(s.AdminKey),
		KycKey:            mirrorKey(s.KycKey),
		FreezeKey:         mirrorKey(s.FreezeKey),
		WipeKey:           mirrorKey(s.WipeKey),
		SupplyKey:         mirrorKey(s.SupplyKey),
		PauseKey:          mirrorKey(s.PauseKey),
		FeeScheduleKey:    mirrorKey(s.FeeScheduleKey),
		MetadataKey:       mirrorKey(s.MetadataKey),
		FreezeDefault:     s.FreezeDefault,
		PauseStatus:       pauseNames[pauseStatus(tok)],
		Deleted:           tok.deleted,
		CustomFees:        renderCustomFees(tok),
		CreatedTimestamp:  mirrorTime(tok.created),
	}
	if !tok.fungible() {
		out.Type = "NON_FUNGIBLE_UNIQUE"
	}
	if s.SupplyType == services.TokenSupplyType_FINITE {
		out.SupplyType = "FINITE"
	}
	return out
}

func renderFixedFee(f *services.FixedFee, collector int64, allExempt bool) mirror.FixedFee {
	out := mirror.FixedFee{
		Amount:              f.GetAmount(),
		CollectorAccountID:  entityString(collector),
		AllCollectorsExempt: allExempt,
	}
	if d := f.GetDenominatingTokenId(); d != nil {
		out.DenominatingTokenID = stringPtr(entityString(d.TokenNum))
	}
	return out
}

func renderCustomFees(tok *token) mirror.CustomFees {
	out := mirror.CustomFees{
		CreatedTimestamp: mirrorTime(tok.created),
		FixedFees:        []mirror.FixedFee{},
		FractionalFees:   []mirror.FractionalFee{},
		RoyaltyFees:      []mirror.RoyaltyFee{},
	}
	for _, fee := range tok.fees {
		collector := fee.FeeCollectorAccountId.GetAccountNum()
		switch f := fee.Fee.(type) {
		case *services.CustomFee_FixedFee:
			out.FixedFees = append(out.FixedFees, renderFixedFee(f.FixedFee, collector, fee.AllCollectorsAreExempt))
		case *services.CustomFee_FractionalFee:
			frac := f.FractionalFee
			row := mirror.FractionalFee{
				Amount:              mirror.Fraction{Numerator: frac.GetFractionalAmount().GetNumerator(), Denominator: frac.GetFractionalAmount().GetDenominator()},
				CollectorAccountID:  entityString(collector),
				Minimum:             frac.MinimumAmount,
				NetOfTransfers:      frac.NetOfTransfers,
				AllCollectorsExempt: fee.AllCollectorsAreExempt,
			}
			if frac.MaximumAmount > 0 {
				most := frac.MaximumAmount
				row.Maximum = &most
			}
			out.FractionalFees = append(out.FractionalFees, row)
		case *services.CustomFee_RoyaltyFee:
			royalty := f.RoyaltyFee
			row := mirror.RoyaltyFee{
				Amount:              mirror.Fraction{Numerator: royalty.GetExchangeValueFraction().GetNumerator(), Denominator: royalty.GetExchangeValueFraction().GetDenominator()},
				CollectorAccountID:  entityString(collector),
				AllCollectorsExempt: fee.AllCollectorsAreExempt,
			}
			if royalty.FallbackFee != nil {
				fallback := renderFixedFee(royalty.FallbackFee, collector, fee.AllCollectorsAreExempt)
				row.FallbackFee = &fallback
			}
			out.RoyaltyFees = append(out.RoyaltyFees, row)
		}
	}
	return out
}

func renderNft(tok *token, n *nft) mirror.Nft {
	return mirror.Nft{
		AccountID:        entityString(n.owner),
		TokenID:          entityString(tok.num),
		SerialNumber:     n.serial,
		Metadata:         base64.StdEncoding.EncodeToString(n.metadata),
		CreatedTimestamp: mirrorTime(n.created),
	}
}

func renderTopic(tp *topic) mirror.Topic {
	return mirror.Topic{
		TopicID:          entityString(tp.num),
		Memo:             tp.memo,
		AdminKey:         mirrorKey(tp.adminKey),
		SubmitKey:        mirrorKey(tp.submitKey),
		Deleted:          tp.deleted,
		CreatedTimestamp: mirrorTime(tp.created),
	}
}

func renderTopicMessage(m *topicMessage) mirror.TopicMessage {
	out := mirror.TopicMessage{
		ConsensusTimestamp: mirrorTime(m.consensus),
		Message:            base64.StdEncoding.EncodeToString(m.message),
		PayerAccountID:     entityString(m.payer),
		RunningHash:        base64.StdEncoding.EncodeToString(m.runningHash),
		RunningHashVersion: runningHashVersion,
		SequenceNumber:     int64(m.sequence),
		TopicID:            entityString(m.topic),
	}
	if c := m.chunk; c != nil {
		initial := c.GetInitialTransactionID()
		start := initial.GetTransactionValidStart()
		out.ChunkInfo = &mirror.ChunkInfo{
			InitialTransactionID: map[string]any{
				"account_id":              entityString(initial.GetAccountID().GetAccountNum()),
				"nonce":                   initial.GetNonce(),
				"scheduled":               initial.GetScheduled(),
				"transaction_valid_start": fmt.Sprintf("%d.%09d", start.GetSeconds(), start.GetNanos()),
			},
			Number: c.Number,
			Total:  c.Total,
		}
	}
	return out
}

func renderSchedule(sc *schedule) mirror.Schedule {
	body, _ := proto.Marshal(sc.body)
	out := mirror.Schedule{
		ScheduleID:         entityString(sc.num),
		CreatorAccountID:   entityString(sc.creator),
		PayerAccountID:     entityString(sc.payer),
		AdminKey:           mirrorKey(sc.adminKey),
		Memo:               sc.memo,
		ConsensusTimestamp: mirrorTime(sc.created),
		ExecutedTimestamp:  mirrorTimePtr(sc.executed),
		ExpirationTime:     mirrorTimePtr(sc.expiration),
		WaitForExpiry:      sc.waitForExpiry,
		Deleted:            !sc.deleted.IsZero(),
		TransactionBody:    base64.StdEncoding.EncodeToString(body),
		Signatures:         []mirror.ScheduleSignature{},
	}
	for _, sig := range sc.signatures {
		row := mirror.ScheduleSignature{
			ConsensusTimestamp: mirrorTime(sig.consensus),
			Signature:          base64.StdEncoding.EncodeToString(sig.sig),
			Type:               "ED25519",
		}
		switch k := sig.key.GetKey().(type) {
		case *services.Key_Ed25519:
			row.PublicKeyPrefix = base64.StdEncoding.EncodeToString(k.Ed25519)
		case *services.Key_ECDSASecp256K1:
			row.PublicKeyPrefix = base64.StdEncoding.EncodeToString(k.ECDSASecp256K1)
			row.Type = "ECDSA_SECP256K1"
		}
		out.Signatures = append(out.Signatures, row)
	}
	return out
}

func renderAirdrop(p *pendingAirdrop) mirror.TokenAirdrop {
	out := mirror.TokenAirdrop{
		Amount:     p.amount,
		ReceiverID: entityString(p.receiver),
		SenderID:   entityString(p.sender),
		TokenID:    entityString(p.token),
		Timestamp:  mirror.Timespan{From: mirrorTime(p.created)},
	}
	if p.serial != 0 {
		serial := p.serial
		out.SerialNumber = &serial
	}
	return out
}

func renderTransaction(r *record) mirror.Transaction {
	out := mirror.Transaction{
		TransactionID:       r.id.mirrorID(),
		Name:                r.name,
		Result:              r.receipt.Status.String(),
		ConsensusTimestamp:  mirrorTime(r.consensus),
		ValidStartTimestamp: fmt.Sprintf("%d.%09d", r.id.seconds, r.id.nanos),
		MaxFee:              strconv.FormatUint(r.maxFee, 10),
		MemoBase64:          base64.StdEncoding.EncodeToString([]byte(r.memo)),
		Nonce:               r.id.nonce,
		Scheduled:           r.id.scheduled,
		Transfers:           []mirror.Transfer{},
		TokenTransfers:      []mirror.TokenTransfer{},
		NftTransfers:        []mirror.NftTransfer{},
		AssessedCustomFees:  []mirror.AssessedCustomFee{},
	}
	if !r.id.scheduled {
		out.Node = stringPtr(entityString(nodeAccount))
	}
	if r.entity != 0 {
		out.EntityID = stringPtr(entityString(r.entity))
	}
	for _, aa := range r.transfers {
		out.Transfers = append(out.Transfers, mirror.Transfer{
			Account: entityString(aa.AccountID.GetAccountNum()),
			Amount:  aa.Amount,
		})
	}
	for _, list := range r.tokenTransfers {
		token := entityString(list.Token.TokenNum)
		for _, aa := range list.Transfers {
			out.TokenTransfers = append(out.TokenTransfers, mirror.TokenTransfer{
				TokenID: token,
				Account: entityString(aa.AccountID.GetAccountNum()),
				Amount:  aa.Amount,
			})
		}
		for _, n := range list.NftTransfers {
			out.NftTransfers = append(out.NftTransfers, mirror.NftTransfer{
				TokenID:           token,
				SerialNumber:      n.SerialNumber,
				SenderAccountID:   optionalAccount(n.SenderAccountID),
				ReceiverAccountID: optionalAccount(n.ReceiverAccountID),
			})
		}
	}
	for _, fee := range r.assessedFees {
		row := mirror.AssessedCustomFee{
			Amount:                   fee.Amount,
			CollectorAccountID:       entityString(fee.FeeCollectorAccountId.GetAccountNum()),
			EffectivePayerAccountIDs: []string{},
		}
		for _, payer := range fee.EffectivePayerAccountId {
			row.EffectivePayerAccountIDs = append(row.EffectivePayerAccountIDs, entityString(payer.GetAccountNum()))
		}
		if fee.TokenId != nil {
			row.TokenID = stringPtr(entityString(fee.TokenId.TokenNum))
		}
		out.AssessedCustomFees = append(out.AssessedCustomFees, row)
	}
	return out
}

// optionalAccount renders 0.0.0, used for mint and burn legs, as null.
func optionalAccount(id *services.AccountID) *string {
	if id.GetAccountNum() == 0 {
		return nil
	}
	return stringPtr(entityString(id.GetAccountNum()))
}
