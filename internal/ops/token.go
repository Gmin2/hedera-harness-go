package ops

import (
	"fmt"
	"strconv"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/event"
)

type tokenCreate struct {
	Common         `yaml:",inline"`
	As             string      `yaml:"as"`
	Name           string      `yaml:"name"`
	Symbol         string      `yaml:"symbol"`
	Type           string      `yaml:"type"` // fungible (default) or nft
	Decimals       uint        `yaml:"decimals"`
	InitialSupply  uint64      `yaml:"initial_supply"`
	MaxSupply      int64       `yaml:"max_supply"` // 0 = infinite
	Treasury       string      `yaml:"treasury"`
	AdminKey       string      `yaml:"admin_key"`
	SupplyKey      string      `yaml:"supply_key"`
	KycKey         string      `yaml:"kyc_key"`
	FreezeKey      string      `yaml:"freeze_key"`
	WipeKey        string      `yaml:"wipe_key"`
	PauseKey       string      `yaml:"pause_key"`
	FeeScheduleKey string      `yaml:"fee_schedule_key"`
	MetadataKey    string      `yaml:"metadata_key"`
	FreezeDefault  bool        `yaml:"freeze_default"`
	TokenMemo      string      `yaml:"token_memo"`
	CustomFees     []customFee `yaml:"custom_fees"`
}

type customFee struct {
	Fixed      *fixedFee      `yaml:"fixed"`
	Fractional *fractionalFee `yaml:"fractional"`
	Royalty    *royaltyFee    `yaml:"royalty"`
}

type fixedFee struct {
	Hbar      float64 `yaml:"hbar"`
	Amount    int64   `yaml:"amount"`
	Token     string  `yaml:"token"` // a token name, or self for the token being created
	Collector string  `yaml:"collector"`
}

type fractionalFee struct {
	Numerator      int64  `yaml:"numerator"`
	Denominator    int64  `yaml:"denominator"`
	Min            int64  `yaml:"min"`
	Max            int64  `yaml:"max"`
	NetOfTransfers bool   `yaml:"net_of_transfers"`
	Collector      string `yaml:"collector"`
}

type royaltyFee struct {
	Numerator    int64   `yaml:"numerator"`
	Denominator  int64   `yaml:"denominator"`
	FallbackHbar float64 `yaml:"fallback_hbar"`
	Collector    string  `yaml:"collector"`
}

func (o *tokenCreate) isNft() bool {
	t := strings.ToLower(o.Type)
	return t == "nft" || t == "non_fungible" || t == "non_fungible_unique"
}

func (o *tokenCreate) Target() string { return o.As }

func (o *tokenCreate) Params() []event.Param {
	p := []event.Param{param("symbol", o.Symbol)}
	if o.isNft() {
		p = append(p, param("type", "nft"))
	} else {
		p = append(p, param("supply", o.InitialSupply), param("decimals", o.Decimals))
	}
	for _, k := range []struct{ name, ref string }{{"kyc", o.KycKey}, {"freeze", o.FreezeKey}, {"pause", o.PauseKey}} {
		if k.ref != "" && k.ref != "none" {
			p = append(p, param(k.name, k.ref))
		}
	}
	if len(o.CustomFees) > 0 {
		p = append(p, param("fees", len(o.CustomFees)))
	}
	return p
}

func (o *tokenCreate) Build(env *Env) (*Tx, error) {
	if o.As == "" {
		return nil, fmt.Errorf("token.create needs as: <name> so later steps can refer to it")
	}
	if o.Name == "" || o.Symbol == "" {
		return nil, fmt.Errorf("token.create needs name and symbol")
	}
	treasuryRef := orOperator(o.Treasury)
	treasury, err := env.Account(treasuryRef)
	if err != nil {
		return nil, err
	}
	tx := hiero.NewTokenCreateTransaction().
		SetTokenName(o.Name).
		SetTokenSymbol(o.Symbol).
		SetTreasuryAccountID(treasury).
		SetFreezeDefault(o.FreezeDefault)

	supplyKey := o.SupplyKey
	if o.isNft() {
		tx.SetTokenType(hiero.TokenTypeNonFungibleUnique)
		if o.InitialSupply != 0 || o.Decimals != 0 {
			return nil, fmt.Errorf("nft tokens have no initial_supply or decimals, mint them instead")
		}
		if supplyKey == "" {
			// an nft collection without a supply key could never mint anything
			supplyKey = treasuryRef
		}
	} else {
		tx.SetDecimals(o.Decimals).SetInitialSupply(o.InitialSupply)
	}
	if o.MaxSupply > 0 {
		tx.SetSupplyType(hiero.TokenSupplyTypeFinite).SetMaxSupply(o.MaxSupply)
	}
	if o.TokenMemo != "" {
		tx.SetTokenMemo(o.TokenMemo)
	}

	setKey := func(ref string, set func(hiero.Key)) error {
		if ref == "" || ref == "none" {
			return nil
		}
		k, err := env.PublicKey(ref)
		if err != nil {
			return err
		}
		set(k)
		return nil
	}
	keySetters := []struct {
		ref string
		set func(hiero.Key)
	}{
		{o.AdminKey, func(k hiero.Key) { tx.SetAdminKey(k) }},
		{supplyKey, func(k hiero.Key) { tx.SetSupplyKey(k) }},
		{o.KycKey, func(k hiero.Key) { tx.SetKycKey(k) }},
		{o.FreezeKey, func(k hiero.Key) { tx.SetFreezeKey(k) }},
		{o.WipeKey, func(k hiero.Key) { tx.SetWipeKey(k) }},
		{o.PauseKey, func(k hiero.Key) { tx.SetPauseKey(k) }},
		{o.FeeScheduleKey, func(k hiero.Key) { tx.SetFeeScheduleKey(k) }},
		{o.MetadataKey, func(k hiero.Key) { tx.SetMetadataKey(k) }},
	}
	for _, ks := range keySetters {
		if err := setKey(ks.ref, ks.set); err != nil {
			return nil, err
		}
	}

	fees, err := o.buildFees(env)
	if err != nil {
		return nil, err
	}
	if len(fees) > 0 {
		tx.SetCustomFees(fees)
	}

	adminSigner := o.AdminKey
	if adminSigner == "none" {
		adminSigner = ""
	}
	// the network wants custom fee collectors to sign the token create
	defaults := []string{o.Treasury, adminSigner}
	for _, f := range o.CustomFees {
		switch {
		case f.Fixed != nil:
			defaults = append(defaults, actorOnly(env, f.Fixed.Collector))
		case f.Fractional != nil:
			defaults = append(defaults, actorOnly(env, f.Fractional.Collector))
		case f.Royalty != nil:
			defaults = append(defaults, actorOnly(env, f.Royalty.Collector))
		}
	}
	return &Tx{
		Tx:           tx,
		Signers:      signers(&o.Common, defaults...),
		Declares:     o.As,
		DeclaresKind: "token",
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			if r.TokenID == nil {
				return nil, fmt.Errorf("receipt has no token id")
			}
			if ref := actorOnly(env, supplyKey); ref != "" {
				env.supplyKeys[o.As] = ref
			}
			return []event.Param{param(o.As, r.TokenID)}, env.Bind(o.As, "token", r.TokenID.String())
		},
	}, nil
}

// actorOnly keeps a reference only when it names an actor hh holds keys for.
func actorOnly(env *Env, ref string) string {
	if ref == "" || ref == "none" {
		return ""
	}
	if _, err := env.Actor(ref); err != nil {
		return ""
	}
	return ref
}

func (o *tokenCreate) buildFees(env *Env) ([]hiero.Fee, error) {
	var fees []hiero.Fee
	for i, f := range o.CustomFees {
		n := 0
		for _, set := range []bool{f.Fixed != nil, f.Fractional != nil, f.Royalty != nil} {
			if set {
				n++
			}
		}
		if n != 1 {
			return nil, fmt.Errorf("custom_fees[%d] must be exactly one of fixed, fractional, royalty", i)
		}
		switch {
		case f.Fixed != nil:
			collector, err := env.Account(orOperator(f.Fixed.Collector))
			if err != nil {
				return nil, err
			}
			fee := hiero.NewCustomFixedFee().SetFeeCollectorAccountID(collector)
			switch {
			case f.Fixed.Hbar > 0:
				fee.SetHbarAmount(hbar(f.Fixed.Hbar))
			case f.Fixed.Token == "self":
				fee.SetAmount(f.Fixed.Amount).SetDenominatingTokenToSameToken()
			case f.Fixed.Token != "":
				tid, err := env.Token(f.Fixed.Token)
				if err != nil {
					return nil, err
				}
				fee.SetAmount(f.Fixed.Amount).SetDenominatingTokenID(tid)
			default:
				return nil, fmt.Errorf("custom_fees[%d].fixed needs hbar, or amount with token", i)
			}
			fees = append(fees, fee)
		case f.Fractional != nil:
			ff := f.Fractional
			if ff.Denominator == 0 {
				return nil, fmt.Errorf("custom_fees[%d].fractional needs a denominator", i)
			}
			collector, err := env.Account(orOperator(ff.Collector))
			if err != nil {
				return nil, err
			}
			method := hiero.FeeAssessmentMethodInclusive
			if ff.NetOfTransfers {
				method = hiero.FeeAssessmentMethodExclusive
			}
			fees = append(fees, hiero.NewCustomFractionalFee().
				SetNumerator(ff.Numerator).SetDenominator(ff.Denominator).
				SetMin(ff.Min).SetMax(ff.Max).
				SetAssessmentMethod(method).
				SetFeeCollectorAccountID(collector))
		case f.Royalty != nil:
			rf := f.Royalty
			if rf.Denominator == 0 {
				return nil, fmt.Errorf("custom_fees[%d].royalty needs a denominator", i)
			}
			collector, err := env.Account(orOperator(rf.Collector))
			if err != nil {
				return nil, err
			}
			fee := hiero.NewCustomRoyaltyFee().
				SetNumerator(rf.Numerator).SetDenominator(rf.Denominator)
			fee.SetFeeCollectorAccountID(collector)
			if rf.FallbackHbar > 0 {
				fee.SetFallbackFee(hiero.NewCustomFixedFee().SetHbarAmount(hbar(rf.FallbackHbar)).SetFeeCollectorAccountID(collector))
			}
			fees = append(fees, fee)
		}
	}
	return fees, nil
}

type tokenAssociate struct {
	Common  `yaml:",inline"`
	Account string   `yaml:"account"`
	Tokens  []string `yaml:"tokens"`
	Token   string   `yaml:"token"`
}

func (o *tokenAssociate) list() []string {
	if o.Token != "" {
		return append([]string{o.Token}, o.Tokens...)
	}
	return o.Tokens
}

func (o *tokenAssociate) Target() string { return o.Account }
func (o *tokenAssociate) Params() []event.Param {
	return []event.Param{param("tokens", strings.Join(o.list(), ","))}
}

func (o *tokenAssociate) Build(env *Env) (*Tx, error) {
	acct, err := env.Account(o.Account)
	if err != nil {
		return nil, err
	}
	names := o.list()
	if len(names) == 0 {
		return nil, fmt.Errorf("token.associate needs token or tokens")
	}
	var ids []hiero.TokenID
	for _, n := range names {
		id, err := env.Token(n)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	tx := hiero.NewTokenAssociateTransaction().SetAccountID(acct).SetTokenIDs(ids...)
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.Account)}, nil
}

type tokenMint struct {
	Common   `yaml:",inline"`
	Token    string   `yaml:"token"`
	Amount   uint64   `yaml:"amount"`
	Metadata []string `yaml:"metadata"` // one entry per nft serial
	Signer   string   `yaml:"supply_key"`
}

func (o *tokenMint) Target() string { return o.Token }

// supplySigner is the actor named in the step, else the supply key the token
// was created with, else the operator.
func (o *tokenMint) supplySigner(env *Env) string {
	if o.Signer != "" {
		return o.Signer
	}
	if ref, ok := env.supplyKeys[o.Token]; ok {
		return ref
	}
	return "operator"
}
func (o *tokenMint) Params() []event.Param {
	if len(o.Metadata) > 0 {
		return []event.Param{param("nfts", len(o.Metadata))}
	}
	return []event.Param{param("amount", o.Amount)}
}

func (o *tokenMint) Build(env *Env) (*Tx, error) {
	id, err := env.Token(o.Token)
	if err != nil {
		return nil, err
	}
	tx := hiero.NewTokenMintTransaction().SetTokenID(id)
	switch {
	case len(o.Metadata) > 0 && o.Amount > 0:
		return nil, fmt.Errorf("token.mint takes amount (fungible) or metadata (nft), not both")
	case len(o.Metadata) > 0:
		var meta [][]byte
		for _, m := range o.Metadata {
			meta = append(meta, []byte(m))
		}
		tx.SetMetadatas(meta)
	case o.Amount > 0:
		tx.SetAmount(o.Amount)
	default:
		return nil, fmt.Errorf("token.mint needs amount or metadata")
	}
	return &Tx{
		Tx:      tx,
		Signers: signers(&o.Common, o.supplySigner(env)),
		Bind: func(env *Env, r hiero.TransactionReceipt) ([]event.Param, error) {
			if len(r.SerialNumbers) > 0 {
				var s []string
				for _, n := range r.SerialNumbers {
					s = append(s, strconv.FormatInt(n, 10))
				}
				return []event.Param{param("serials", strings.Join(s, ","))}, nil
			}
			return []event.Param{param("total_supply", r.TotalSupply)}, nil
		},
	}, nil
}

// tokenMove covers token.transfer and token.airdrop, which take the same fields.
type tokenMove struct {
	Common `yaml:",inline"`
	Token  string `yaml:"token"`
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Amount int64  `yaml:"amount"`
	Serial int64  `yaml:"serial"`
	// Recipients sends to several accounts in one transaction.
	Recipients []recipient `yaml:"recipients"`
	airdrop    bool
}

type recipient struct {
	To     string `yaml:"to"`
	Amount int64  `yaml:"amount"`
	Serial int64  `yaml:"serial"`
}

func (o *tokenMove) legs() []recipient {
	if len(o.Recipients) > 0 {
		return o.Recipients
	}
	return []recipient{{To: o.To, Amount: o.Amount, Serial: o.Serial}}
}

func (o *tokenMove) Target() string { return o.Token }
func (o *tokenMove) Params() []event.Param {
	p := []event.Param{param("from", orOperator(o.From))}
	if len(o.Recipients) > 0 {
		return append(p, param("recipients", len(o.Recipients)))
	}
	p = append(p, param("to", o.To))
	if o.Serial > 0 {
		return append(p, param("serial", o.Serial))
	}
	return append(p, param("amount", o.Amount))
}

func (o *tokenMove) Build(env *Env) (*Tx, error) {
	tid, err := env.Token(o.Token)
	if err != nil {
		return nil, err
	}
	from, err := env.Account(orOperator(o.From))
	if err != nil {
		return nil, err
	}
	if len(o.Recipients) > 0 && (o.To != "" || o.Amount != 0 || o.Serial != 0) {
		return nil, fmt.Errorf("use either to with amount/serial, or recipients")
	}

	type adder interface {
		addToken(tid hiero.TokenID, acct hiero.AccountID, amount int64)
		addNft(id hiero.NftID, from, to hiero.AccountID)
	}
	var tx hiero.TransactionInterface
	var add adder
	if o.airdrop {
		t := hiero.NewTokenAirdropTransaction()
		tx, add = t, airdropAdder{t}
	} else {
		t := hiero.NewTransferTransaction()
		tx, add = t, transferAdder{t}
	}
	for _, leg := range o.legs() {
		to, err := env.Account(leg.To)
		if err != nil {
			return nil, err
		}
		if (leg.Serial > 0) == (leg.Amount != 0) {
			return nil, fmt.Errorf("give amount for fungible tokens or serial for an nft")
		}
		if leg.Serial > 0 {
			add.addNft(hiero.NftID{TokenID: tid, SerialNumber: leg.Serial}, from, to)
			continue
		}
		add.addToken(tid, from, -leg.Amount)
		add.addToken(tid, to, leg.Amount)
	}
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.From)}, nil
}

type airdropAdder struct {
	t *hiero.TokenAirdropTransaction
}

func (a airdropAdder) addToken(tid hiero.TokenID, acct hiero.AccountID, amount int64) {
	a.t.AddTokenTransfer(tid, acct, amount)
}
func (a airdropAdder) addNft(id hiero.NftID, from, to hiero.AccountID) {
	a.t.AddNftTransfer(id, from, to)
}

type transferAdder struct{ t *hiero.TransferTransaction }

func (a transferAdder) addToken(tid hiero.TokenID, acct hiero.AccountID, amount int64) {
	a.t.AddTokenTransfer(tid, acct, amount)
}
func (a transferAdder) addNft(id hiero.NftID, from, to hiero.AccountID) {
	a.t.AddNftTransfer(id, from, to)
}

// pendingAirdrop covers token.claim and token.cancel.
type pendingAirdrop struct {
	Common `yaml:",inline"`
	Token  string `yaml:"token"`
	From   string `yaml:"from"` // airdrop sender
	To     string `yaml:"to"`   // airdrop receiver
	Serial int64  `yaml:"serial"`
	cancel bool
}

func (o *pendingAirdrop) Target() string { return o.Token }
func (o *pendingAirdrop) Params() []event.Param {
	p := []event.Param{param("from", orOperator(o.From)), param("to", o.To)}
	if o.Serial > 0 {
		p = append(p, param("serial", o.Serial))
	}
	return p
}

func (o *pendingAirdrop) Build(env *Env) (*Tx, error) {
	tid, err := env.Token(o.Token)
	if err != nil {
		return nil, err
	}
	sender, err := env.Account(orOperator(o.From))
	if err != nil {
		return nil, err
	}
	receiver, err := env.Account(o.To)
	if err != nil {
		return nil, err
	}
	id := &hiero.PendingAirdropId{}
	id.SetSender(sender).SetReceiver(receiver)
	if o.Serial > 0 {
		id.SetNftID(hiero.NftID{TokenID: tid, SerialNumber: o.Serial})
	} else {
		id.SetTokenID(tid)
	}
	if o.cancel {
		tx := hiero.NewTokenCancelAirdropTransaction().AddPendingAirdropId(*id)
		return &Tx{Tx: tx, Signers: signers(&o.Common, o.From)}, nil
	}
	tx := hiero.NewTokenClaimAirdropTransaction().AddPendingAirdropId(*id)
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.To)}, nil
}

// tokenGate covers kyc, freeze and pause switches.
type tokenGate struct {
	Common  `yaml:",inline"`
	Token   string `yaml:"token"`
	Account string `yaml:"account"`
	Signer  string `yaml:"key"` // actor holding the kyc / freeze / pause key
	kind    string
}

func (o *tokenGate) Target() string { return o.Token }
func (o *tokenGate) Params() []event.Param {
	if o.Account == "" {
		return nil
	}
	return []event.Param{param("account", o.Account)}
}

func (o *tokenGate) Build(env *Env) (*Tx, error) {
	tid, err := env.Token(o.Token)
	if err != nil {
		return nil, err
	}
	var acct hiero.AccountID
	needsAccount := o.kind != "pause" && o.kind != "unpause"
	if needsAccount {
		if acct, err = env.Account(o.Account); err != nil {
			return nil, err
		}
	}
	var tx hiero.TransactionInterface
	switch o.kind {
	case "grant_kyc":
		tx = hiero.NewTokenGrantKycTransaction().SetTokenID(tid).SetAccountID(acct)
	case "revoke_kyc":
		tx = hiero.NewTokenRevokeKycTransaction().SetTokenID(tid).SetAccountID(acct)
	case "freeze":
		tx = hiero.NewTokenFreezeTransaction().SetTokenID(tid).SetAccountID(acct)
	case "unfreeze":
		tx = hiero.NewTokenUnfreezeTransaction().SetTokenID(tid).SetAccountID(acct)
	case "pause":
		tx = hiero.NewTokenPauseTransaction().SetTokenID(tid)
	case "unpause":
		tx = hiero.NewTokenUnpauseTransaction().SetTokenID(tid)
	}
	return &Tx{Tx: tx, Signers: signers(&o.Common, orOperator(o.Signer))}, nil
}

type tokenReject struct {
	Common `yaml:",inline"`
	Owner  string `yaml:"owner"`
	Token  string `yaml:"token"`
	Serial int64  `yaml:"serial"`
}

func (o *tokenReject) Target() string        { return o.Token }
func (o *tokenReject) Params() []event.Param { return []event.Param{param("owner", o.Owner)} }

func (o *tokenReject) Build(env *Env) (*Tx, error) {
	tid, err := env.Token(o.Token)
	if err != nil {
		return nil, err
	}
	owner, err := env.Account(o.Owner)
	if err != nil {
		return nil, err
	}
	tx := hiero.NewTokenRejectTransaction().SetOwnerID(owner)
	if o.Serial > 0 {
		tx.AddNftID(hiero.NftID{TokenID: tid, SerialNumber: o.Serial})
	} else {
		tx.AddTokenID(tid)
	}
	return &Tx{Tx: tx, Signers: signers(&o.Common, o.Owner)}, nil
}

func init() {
	register("token.create", func() Op { return &tokenCreate{} })
	register("token.associate", func() Op { return &tokenAssociate{} })
	register("token.mint", func() Op { return &tokenMint{} })
	register("token.transfer", func() Op { return &tokenMove{} })
	register("token.airdrop", func() Op { return &tokenMove{airdrop: true} })
	register("token.claim", func() Op { return &pendingAirdrop{} })
	register("token.cancel", func() Op { return &pendingAirdrop{cancel: true} })
	register("token.reject", func() Op { return &tokenReject{} })
	for _, k := range []string{"grant_kyc", "revoke_kyc", "freeze", "unfreeze", "pause", "unpause"} {
		kind := k
		register("token."+kind, func() Op { return &tokenGate{kind: kind} })
	}
}
