package mock

import (
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
)

// operation picks the handler for the body type. The grpc method the sdk used
// is ignored on purpose, AccountDelete for example arrives on ApproveAllowances.
func (l *ledger) operation(t *txn) (operation, bool) {
	switch d := t.body.Data.(type) {
	case *services.TransactionBody_CryptoCreateAccount:
		b := d.CryptoCreateAccount
		return operation{"CRYPTOCREATEACCOUNT",
			func() ([]*services.Key, code) { return l.createAccountKeys(b) },
			func() code { return l.createAccount(t, b) }}, true
	case *services.TransactionBody_CryptoUpdateAccount:
		b := d.CryptoUpdateAccount
		return operation{"CRYPTOUPDATEACCOUNT",
			func() ([]*services.Key, code) { return l.updateAccountKeys(b) },
			func() code { return l.updateAccount(b) }}, true
	case *services.TransactionBody_CryptoDelete:
		b := d.CryptoDelete
		return operation{"CRYPTODELETE",
			func() ([]*services.Key, code) { return l.deleteAccountKeys(b) },
			func() code { return l.deleteAccount(t, b) }}, true
	case *services.TransactionBody_CryptoTransfer:
		b := d.CryptoTransfer
		return operation{"CRYPTOTRANSFER",
			func() ([]*services.Key, code) { return l.transferKeys(b) },
			func() code { return l.cryptoTransfer(t, b) }}, true

	case *services.TransactionBody_TokenCreation:
		b := d.TokenCreation
		return operation{"TOKENCREATION",
			func() ([]*services.Key, code) { return l.createTokenKeys(b) },
			func() code { return l.createToken(t, b) }}, true
	case *services.TransactionBody_TokenAssociate:
		b := d.TokenAssociate
		return operation{"TOKENASSOCIATE",
			func() ([]*services.Key, code) { return l.accountKey(b.Account) },
			func() code { return l.associate(t, b) }}, true
	case *services.TransactionBody_TokenDissociate:
		b := d.TokenDissociate
		return operation{"TOKENDISSOCIATE",
			func() ([]*services.Key, code) { return l.accountKey(b.Account) },
			func() code { return l.dissociate(t, b) }}, true
	case *services.TransactionBody_TokenMint:
		b := d.TokenMint
		return operation{"TOKENMINT",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, supplyKey) },
			func() code { return l.mint(t, b) }}, true
	case *services.TransactionBody_TokenBurn:
		b := d.TokenBurn
		return operation{"TOKENBURN",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, supplyKey) },
			func() code { return l.burn(t, b) }}, true
	case *services.TransactionBody_TokenGrantKyc:
		b := d.TokenGrantKyc
		return operation{"TOKENGRANTKYC",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, kycKey) },
			func() code { return l.setKyc(t, b.Token, b.Account, services.TokenKycStatus_Granted) }}, true
	case *services.TransactionBody_TokenRevokeKyc:
		b := d.TokenRevokeKyc
		return operation{"TOKENREVOKEKYC",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, kycKey) },
			func() code { return l.setKyc(t, b.Token, b.Account, services.TokenKycStatus_Revoked) }}, true
	case *services.TransactionBody_TokenFreeze:
		b := d.TokenFreeze
		return operation{"TOKENFREEZE",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, freezeKey) },
			func() code { return l.setFreeze(t, b.Token, b.Account, services.TokenFreezeStatus_Frozen) }}, true
	case *services.TransactionBody_TokenUnfreeze:
		b := d.TokenUnfreeze
		return operation{"TOKENUNFREEZE",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, freezeKey) },
			func() code { return l.setFreeze(t, b.Token, b.Account, services.TokenFreezeStatus_Unfrozen) }}, true
	case *services.TransactionBody_TokenPause:
		b := d.TokenPause
		return operation{"TOKENPAUSE",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, pauseKey) },
			func() code { return l.setPaused(t, b.Token, true) }}, true
	case *services.TransactionBody_TokenUnpause:
		b := d.TokenUnpause
		return operation{"TOKENUNPAUSE",
			func() ([]*services.Key, code) { return l.tokenKey(b.Token, pauseKey) },
			func() code { return l.setPaused(t, b.Token, false) }}, true

	case *services.TransactionBody_TokenAirdrop:
		b := d.TokenAirdrop
		return operation{"TOKENAIRDROP",
			func() ([]*services.Key, code) { return l.airdropKeys(b) },
			func() code { return l.airdrop(t, b) }}, true
	case *services.TransactionBody_TokenClaimAirdrop:
		b := d.TokenClaimAirdrop
		return operation{"TOKENCLAIMAIRDROP",
			func() ([]*services.Key, code) { return l.pendingAirdropKeys(b.PendingAirdrops, true) },
			func() code { return l.claimAirdrops(t, b.PendingAirdrops) }}, true
	case *services.TransactionBody_TokenCancelAirdrop:
		b := d.TokenCancelAirdrop
		return operation{"TOKENCANCELAIRDROP",
			func() ([]*services.Key, code) { return l.pendingAirdropKeys(b.PendingAirdrops, false) },
			func() code { return l.cancelAirdrops(b.PendingAirdrops) }}, true
	case *services.TransactionBody_TokenReject:
		b := d.TokenReject
		return operation{"TOKENREJECT",
			func() ([]*services.Key, code) { return l.accountKey(rejectOwner(t, b)) },
			func() code { return l.reject(t, b) }}, true

	case *services.TransactionBody_ConsensusCreateTopic:
		b := d.ConsensusCreateTopic
		return operation{"CONSENSUSCREATETOPIC",
			func() ([]*services.Key, code) { return l.createTopicKeys(b) },
			func() code { return l.createTopic(t, b) }}, true
	case *services.TransactionBody_ConsensusSubmitMessage:
		b := d.ConsensusSubmitMessage
		return operation{"CONSENSUSSUBMITMESSAGE",
			func() ([]*services.Key, code) { return l.submitMessageKeys(b) },
			func() code { return l.submitMessage(t, b) }}, true

	case *services.TransactionBody_ScheduleCreate:
		b := d.ScheduleCreate
		return operation{"SCHEDULECREATE",
			func() ([]*services.Key, code) { return []*services.Key{b.AdminKey}, codeOK },
			func() code { return l.createSchedule(t, b) }}, true
	case *services.TransactionBody_ScheduleSign:
		b := d.ScheduleSign
		return operation{"SCHEDULESIGN", nil,
			func() code { return l.signSchedule(t, b) }}, true
	case *services.TransactionBody_ScheduleDelete:
		b := d.ScheduleDelete
		return operation{"SCHEDULEDELETE",
			func() ([]*services.Key, code) { return l.deleteScheduleKeys(b) },
			func() code { return l.deleteSchedule(t, b) }}, true
	}
	return operation{}, false
}

// scheduledBody lifts a schedulable body into a full transaction body for the
// types the mock can execute from a schedule.
func scheduledBody(s *services.SchedulableTransactionBody) (*services.TransactionBody, bool) {
	body := &services.TransactionBody{Memo: s.GetMemo(), TransactionFee: s.GetTransactionFee()}
	switch d := s.GetData().(type) {
	case *services.SchedulableTransactionBody_CryptoCreateAccount:
		body.Data = &services.TransactionBody_CryptoCreateAccount{CryptoCreateAccount: d.CryptoCreateAccount}
	case *services.SchedulableTransactionBody_CryptoUpdateAccount:
		body.Data = &services.TransactionBody_CryptoUpdateAccount{CryptoUpdateAccount: d.CryptoUpdateAccount}
	case *services.SchedulableTransactionBody_CryptoDelete:
		body.Data = &services.TransactionBody_CryptoDelete{CryptoDelete: d.CryptoDelete}
	case *services.SchedulableTransactionBody_CryptoTransfer:
		body.Data = &services.TransactionBody_CryptoTransfer{CryptoTransfer: d.CryptoTransfer}
	case *services.SchedulableTransactionBody_TokenCreation:
		body.Data = &services.TransactionBody_TokenCreation{TokenCreation: d.TokenCreation}
	case *services.SchedulableTransactionBody_TokenAssociate:
		body.Data = &services.TransactionBody_TokenAssociate{TokenAssociate: d.TokenAssociate}
	case *services.SchedulableTransactionBody_TokenDissociate:
		body.Data = &services.TransactionBody_TokenDissociate{TokenDissociate: d.TokenDissociate}
	case *services.SchedulableTransactionBody_TokenMint:
		body.Data = &services.TransactionBody_TokenMint{TokenMint: d.TokenMint}
	case *services.SchedulableTransactionBody_TokenBurn:
		body.Data = &services.TransactionBody_TokenBurn{TokenBurn: d.TokenBurn}
	case *services.SchedulableTransactionBody_TokenGrantKyc:
		body.Data = &services.TransactionBody_TokenGrantKyc{TokenGrantKyc: d.TokenGrantKyc}
	case *services.SchedulableTransactionBody_TokenRevokeKyc:
		body.Data = &services.TransactionBody_TokenRevokeKyc{TokenRevokeKyc: d.TokenRevokeKyc}
	case *services.SchedulableTransactionBody_TokenFreeze:
		body.Data = &services.TransactionBody_TokenFreeze{TokenFreeze: d.TokenFreeze}
	case *services.SchedulableTransactionBody_TokenUnfreeze:
		body.Data = &services.TransactionBody_TokenUnfreeze{TokenUnfreeze: d.TokenUnfreeze}
	case *services.SchedulableTransactionBody_TokenPause:
		body.Data = &services.TransactionBody_TokenPause{TokenPause: d.TokenPause}
	case *services.SchedulableTransactionBody_TokenUnpause:
		body.Data = &services.TransactionBody_TokenUnpause{TokenUnpause: d.TokenUnpause}
	case *services.SchedulableTransactionBody_TokenAirdrop:
		body.Data = &services.TransactionBody_TokenAirdrop{TokenAirdrop: d.TokenAirdrop}
	case *services.SchedulableTransactionBody_TokenClaimAirdrop:
		body.Data = &services.TransactionBody_TokenClaimAirdrop{TokenClaimAirdrop: d.TokenClaimAirdrop}
	case *services.SchedulableTransactionBody_TokenCancelAirdrop:
		body.Data = &services.TransactionBody_TokenCancelAirdrop{TokenCancelAirdrop: d.TokenCancelAirdrop}
	case *services.SchedulableTransactionBody_TokenReject:
		body.Data = &services.TransactionBody_TokenReject{TokenReject: d.TokenReject}
	case *services.SchedulableTransactionBody_ConsensusCreateTopic:
		body.Data = &services.TransactionBody_ConsensusCreateTopic{ConsensusCreateTopic: d.ConsensusCreateTopic}
	case *services.SchedulableTransactionBody_ConsensusSubmitMessage:
		body.Data = &services.TransactionBody_ConsensusSubmitMessage{ConsensusSubmitMessage: d.ConsensusSubmitMessage}
	default:
		return nil, false
	}
	return body, true
}
