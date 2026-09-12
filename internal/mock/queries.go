package mock

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const autoRenewSeconds = 7776000

// answer serves a query. Every query is free, the header echoes the requested
// response type so the sdk cost round trip and the paid answer both work.
func (l *ledger) answer(q *services.Query) *services.Response {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch v := q.Query.(type) {
	case *services.Query_TransactionGetReceipt:
		return l.receiptQuery(v.TransactionGetReceipt)
	case *services.Query_TransactionGetRecord:
		return l.recordQuery(v.TransactionGetRecord)
	case *services.Query_CryptoGetInfo:
		return l.accountInfoQuery(v.CryptoGetInfo)
	case *services.Query_CryptogetAccountBalance:
		return l.balanceQuery(v.CryptogetAccountBalance)
	case *services.Query_TokenGetInfo:
		return l.tokenInfoQuery(v.TokenGetInfo)
	case *services.Query_ConsensusGetTopicInfo:
		return l.topicInfoQuery(v.ConsensusGetTopicInfo)
	case *services.Query_ScheduleGetInfo:
		return l.scheduleInfoQuery(v.ScheduleGetInfo)
	}
	return unsupportedQuery(q)
}

func responseHeader(h *services.QueryHeader, c code) *services.ResponseHeader {
	return &services.ResponseHeader{NodeTransactionPrecheckCode: c, ResponseType: h.GetResponseType()}
}

func (l *ledger) receiptQuery(q *services.TransactionGetReceiptQuery) *services.Response {
	resp := &services.TransactionGetReceiptResponse{Header: responseHeader(q.Header, codeOK)}
	if q.TransactionID == nil {
		resp.Header.NodeTransactionPrecheckCode = services.ResponseCodeEnum_INVALID_TRANSACTION_ID
	} else if rec := l.records[keyOfTx(q.TransactionID)]; rec != nil {
		resp.Receipt = rec.receipt
	} else {
		resp.Header.NodeTransactionPrecheckCode = services.ResponseCodeEnum_RECEIPT_NOT_FOUND
	}
	return &services.Response{Response: &services.Response_TransactionGetReceipt{TransactionGetReceipt: resp}}
}

func (l *ledger) recordQuery(q *services.TransactionGetRecordQuery) *services.Response {
	resp := &services.TransactionGetRecordResponse{Header: responseHeader(q.Header, codeOK)}
	if q.TransactionID == nil {
		resp.Header.NodeTransactionPrecheckCode = services.ResponseCodeEnum_INVALID_TRANSACTION_ID
	} else if rec := l.records[keyOfTx(q.TransactionID)]; rec != nil {
		resp.TransactionRecord = rec.proto()
	} else {
		resp.Header.NodeTransactionPrecheckCode = services.ResponseCodeEnum_RECORD_NOT_FOUND
	}
	return &services.Response{Response: &services.Response_TransactionGetRecord{TransactionGetRecord: resp}}
}

func (l *ledger) accountInfoQuery(q *services.CryptoGetInfoQuery) *services.Response {
	resp := &services.CryptoGetInfoResponse{Header: responseHeader(q.Header, codeOK)}
	if a, c := l.st.account(q.AccountID); c != codeOK {
		resp.Header.NodeTransactionPrecheckCode = c
	} else {
		info := &services.CryptoGetInfoResponse_AccountInfo{
			AccountID:                     accountProto(a.num),
			ContractAccountID:             strings.TrimPrefix(evmAddress(a), "0x"),
			Deleted:                       a.deleted,
			Key:                           a.key,
			Balance:                       uint64(a.balance),
			ReceiverSigRequired:           a.receiverSigRequired,
			ExpirationTime:                timestampProto(a.created.Add(autoRenewSeconds * time.Second)),
			AutoRenewPeriod:               &services.Duration{Seconds: autoRenewSeconds},
			Memo:                          a.memo,
			MaxAutomaticTokenAssociations: a.maxAutoAssociations,
		}
		for _, num := range slices.Sorted(maps.Keys(a.tokens)) {
			rel, tok := a.tokens[num], l.st.tokens[num]
			info.TokenRelationships = append(info.TokenRelationships, &services.TokenRelationship{
				TokenId:              tokenProto(num),
				Symbol:               tok.spec.Symbol,
				Balance:              uint64(rel.balance),
				KycStatus:            rel.kyc,
				FreezeStatus:         rel.freeze,
				Decimals:             tok.spec.Decimals,
				AutomaticAssociation: rel.automatic,
			})
			if !tok.fungible() {
				info.OwnedNfts += rel.balance
			}
		}
		resp.AccountInfo = info
	}
	return &services.Response{Response: &services.Response_CryptoGetInfo{CryptoGetInfo: resp}}
}

func (l *ledger) balanceQuery(q *services.CryptoGetAccountBalanceQuery) *services.Response {
	resp := &services.CryptoGetAccountBalanceResponse{Header: responseHeader(q.Header, codeOK)}
	a, c := l.st.account(q.GetAccountID())
	if c != codeOK {
		resp.Header.NodeTransactionPrecheckCode = c
	} else {
		resp.AccountID = accountProto(a.num)
		resp.Balance = uint64(a.balance)
		for _, num := range slices.Sorted(maps.Keys(a.tokens)) {
			resp.TokenBalances = append(resp.TokenBalances, &services.TokenBalance{
				TokenId:  tokenProto(num),
				Balance:  uint64(a.tokens[num].balance),
				Decimals: l.st.tokens[num].spec.Decimals,
			})
		}
	}
	return &services.Response{Response: &services.Response_CryptogetAccountBalance{CryptogetAccountBalance: resp}}
}

func (l *ledger) tokenInfoQuery(q *services.TokenGetInfoQuery) *services.Response {
	resp := &services.TokenGetInfoResponse{Header: responseHeader(q.Header, codeOK)}
	tok, found := l.st.tokens[q.GetToken().GetTokenNum()]
	if q.Token == nil || !found {
		resp.Header.NodeTransactionPrecheckCode = services.ResponseCodeEnum_INVALID_TOKEN_ID
	} else {
		s := tok.spec
		info := &services.TokenInfo{
			TokenId:             tokenProto(tok.num),
			Name:                s.Name,
			Symbol:              s.Symbol,
			Decimals:            s.Decimals,
			TotalSupply:         uint64(tok.supply),
			Treasury:            accountProto(tok.treasury),
			AdminKey:            s.AdminKey,
			KycKey:              s.KycKey,
			FreezeKey:           s.FreezeKey,
			WipeKey:             s.WipeKey,
			SupplyKey:           s.SupplyKey,
			FeeScheduleKey:      s.FeeScheduleKey,
			PauseKey:            s.PauseKey,
			MetadataKey:         s.MetadataKey,
			DefaultFreezeStatus: services.TokenFreezeStatus_FreezeNotApplicable,
			DefaultKycStatus:    services.TokenKycStatus_KycNotApplicable,
			Deleted:             tok.deleted,
			AutoRenewAccount:    s.AutoRenewAccount,
			AutoRenewPeriod:     s.AutoRenewPeriod,
			Expiry:              timestampProto(tok.created.Add(autoRenewSeconds * time.Second)),
			Memo:                s.Memo,
			TokenType:           s.TokenType,
			SupplyType:          s.SupplyType,
			MaxSupply:           s.MaxSupply,
			CustomFees:          tok.fees,
			PauseStatus:         pauseStatus(tok),
			Metadata:            s.Metadata,
		}
		if s.FreezeKey != nil {
			info.DefaultFreezeStatus = services.TokenFreezeStatus_Unfrozen
			if s.FreezeDefault {
				info.DefaultFreezeStatus = services.TokenFreezeStatus_Frozen
			}
		}
		if s.KycKey != nil {
			info.DefaultKycStatus = services.TokenKycStatus_Revoked
		}
		resp.TokenInfo = info
	}
	return &services.Response{Response: &services.Response_TokenGetInfo{TokenGetInfo: resp}}
}

func pauseStatus(tok *token) services.TokenPauseStatus {
	switch {
	case tok.spec.PauseKey == nil:
		return services.TokenPauseStatus_PauseNotApplicable
	case tok.paused:
		return services.TokenPauseStatus_Paused
	}
	return services.TokenPauseStatus_Unpaused
}

func (l *ledger) topicInfoQuery(q *services.ConsensusGetTopicInfoQuery) *services.Response {
	resp := &services.ConsensusGetTopicInfoResponse{Header: responseHeader(q.Header, codeOK), TopicID: q.TopicID}
	if tp, c := l.st.topic(q.TopicID); c != codeOK {
		resp.Header.NodeTransactionPrecheckCode = c
	} else {
		info := &services.ConsensusTopicInfo{
			Memo:            tp.memo,
			RunningHash:     tp.runningHash,
			SequenceNumber:  uint64(len(tp.messages)),
			ExpirationTime:  timestampProto(tp.created.Add(autoRenewSeconds * time.Second)),
			AdminKey:        tp.adminKey,
			SubmitKey:       tp.submitKey,
			AutoRenewPeriod: &services.Duration{Seconds: autoRenewSeconds},
		}
		if tp.autoRenew != 0 {
			info.AutoRenewAccount = accountProto(tp.autoRenew)
		}
		resp.TopicInfo = info
	}
	return &services.Response{Response: &services.Response_ConsensusGetTopicInfo{ConsensusGetTopicInfo: resp}}
}

func (l *ledger) scheduleInfoQuery(q *services.ScheduleGetInfoQuery) *services.Response {
	resp := &services.ScheduleGetInfoResponse{Header: responseHeader(q.Header, codeOK)}
	if sc, c := l.st.schedule(q.ScheduleID); c != codeOK {
		resp.Header.NodeTransactionPrecheckCode = c
	} else {
		info := &services.ScheduleInfo{
			ScheduleID:               scheduleProto(sc.num),
			ExpirationTime:           timestampProto(sc.expiration),
			ScheduledTransactionBody: sc.body,
			Memo:                     sc.memo,
			AdminKey:                 sc.adminKey,
			Signers:                  &services.KeyList{},
			CreatorAccountID:         accountProto(sc.creator),
			PayerAccountID:           accountProto(sc.payer),
			ScheduledTransactionID:   sc.scheduledTx.proto(),
			WaitForExpiry:            sc.waitForExpiry,
		}
		for _, sig := range sc.signatures {
			info.Signers.Keys = append(info.Signers.Keys, sig.key)
		}
		switch {
		case !sc.executed.IsZero():
			info.Data = &services.ScheduleInfo_ExecutionTime{ExecutionTime: timestampProto(sc.executed)}
		case !sc.deleted.IsZero():
			info.Data = &services.ScheduleInfo_DeletionTime{DeletionTime: timestampProto(sc.deleted)}
		}
		resp.ScheduleInfo = info
	}
	return &services.Response{Response: &services.Response_ScheduleGetInfo{ScheduleGetInfo: resp}}
}

// unsupportedQuery answers NOT_SUPPORTED inside the response variant that
// matches the query. The sdk reads the header of that exact variant and would
// crash on a nil one, so an empty Response is not an option.
func unsupportedQuery(q *services.Query) *services.Response {
	resp := &services.Response{}
	qr := q.ProtoReflect()
	chosen := qr.WhichOneof(qr.Descriptor().Oneofs().ByName("query"))
	if chosen == nil {
		return resp
	}
	request := qr.Get(chosen).Message()
	var responseType services.ResponseType
	if h, isHeader := request.Get(request.Descriptor().Fields().ByName("header")).Message().Interface().(*services.QueryHeader); isHeader {
		responseType = h.GetResponseType()
	}

	want := strings.TrimSuffix(string(chosen.Message().Name()), "Query") + "Response"
	rr := resp.ProtoReflect()
	fields := rr.Descriptor().Fields()
	for i := range fields.Len() {
		f := fields.Get(i)
		if f.Message() == nil || string(f.Message().Name()) != want {
			continue
		}
		msg := rr.NewField(f).Message()
		header := &services.ResponseHeader{
			NodeTransactionPrecheckCode: services.ResponseCodeEnum_NOT_SUPPORTED,
			ResponseType:                responseType,
		}
		msg.Set(msg.Descriptor().Fields().ByName("header"), protoreflect.ValueOfMessage(header.ProtoReflect()))
		rr.Set(f, protoreflect.ValueOfMessage(msg))
		return resp
	}
	return resp
}
