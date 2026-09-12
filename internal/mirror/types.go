package mirror

// Response shapes follow hedera-docs/openapi.yaml (mirror node rest v0.154).
// Only the fields hh reads are declared. The mock mirror renders the same structs.

type Links struct {
	Next *string `json:"next"`
}

type Key struct {
	Type string `json:"_type"` // ED25519, ECDSA_SECP256K1, ProtobufEncoded
	Key  string `json:"key"`   // hex
}

type TokenBalance struct {
	TokenID string `json:"token_id"`
	Balance int64  `json:"balance"`
}

type AccountBalance struct {
	Balance   int64          `json:"balance"` // tinybars
	Timestamp string         `json:"timestamp"`
	Tokens    []TokenBalance `json:"tokens"`
}

type Account struct {
	Account                       string         `json:"account"`
	Alias                         *string        `json:"alias"`
	EvmAddress                    string         `json:"evm_address"`
	Balance                       AccountBalance `json:"balance"`
	Key                           *Key           `json:"key"`
	MaxAutomaticTokenAssociations int32          `json:"max_automatic_token_associations"`
	ReceiverSigRequired           bool           `json:"receiver_sig_required"`
	Memo                          string         `json:"memo"`
	Deleted                       bool           `json:"deleted"`
	CreatedTimestamp              string         `json:"created_timestamp"`
}

type TokenRelationship struct {
	TokenID              string `json:"token_id"`
	Balance              int64  `json:"balance"`
	Decimals             int64  `json:"decimals"`
	AutomaticAssociation bool   `json:"automatic_association"`
	FreezeStatus         string `json:"freeze_status"` // NOT_APPLICABLE, FROZEN, UNFROZEN
	KycStatus            string `json:"kyc_status"`    // NOT_APPLICABLE, GRANTED, REVOKED
	CreatedTimestamp     string `json:"created_timestamp"`
}

type TokenRelationships struct {
	Tokens []TokenRelationship `json:"tokens"`
	Links  Links               `json:"links"`
}

type FixedFee struct {
	Amount              int64   `json:"amount"`
	CollectorAccountID  string  `json:"collector_account_id"`
	DenominatingTokenID *string `json:"denominating_token_id"`
	AllCollectorsExempt bool    `json:"all_collectors_are_exempt"`
}

type Fraction struct {
	Numerator   int64 `json:"numerator"`
	Denominator int64 `json:"denominator"`
}

type FractionalFee struct {
	Amount              Fraction `json:"amount"`
	CollectorAccountID  string   `json:"collector_account_id"`
	Minimum             int64    `json:"minimum"`
	Maximum             *int64   `json:"maximum"`
	NetOfTransfers      bool     `json:"net_of_transfers"`
	AllCollectorsExempt bool     `json:"all_collectors_are_exempt"`
}

type RoyaltyFee struct {
	Amount              Fraction  `json:"amount"`
	CollectorAccountID  string    `json:"collector_account_id"`
	FallbackFee         *FixedFee `json:"fallback_fee"`
	AllCollectorsExempt bool      `json:"all_collectors_are_exempt"`
}

type CustomFees struct {
	CreatedTimestamp string          `json:"created_timestamp"`
	FixedFees        []FixedFee      `json:"fixed_fees"`
	FractionalFees   []FractionalFee `json:"fractional_fees"`
	RoyaltyFees      []RoyaltyFee    `json:"royalty_fees"`
}

type Token struct {
	TokenID           string     `json:"token_id"`
	Type              string     `json:"type"` // FUNGIBLE_COMMON, NON_FUNGIBLE_UNIQUE
	Name              string     `json:"name"`
	Symbol            string     `json:"symbol"`
	Memo              string     `json:"memo"`
	Decimals          string     `json:"decimals"`
	InitialSupply     string     `json:"initial_supply"`
	TotalSupply       string     `json:"total_supply"`
	MaxSupply         string     `json:"max_supply"`
	SupplyType        string     `json:"supply_type"` // FINITE, INFINITE
	TreasuryAccountID string     `json:"treasury_account_id"`
	AdminKey          *Key       `json:"admin_key"`
	KycKey            *Key       `json:"kyc_key"`
	FreezeKey         *Key       `json:"freeze_key"`
	WipeKey           *Key       `json:"wipe_key"`
	SupplyKey         *Key       `json:"supply_key"`
	PauseKey          *Key       `json:"pause_key"`
	FeeScheduleKey    *Key       `json:"fee_schedule_key"`
	MetadataKey       *Key       `json:"metadata_key"`
	FreezeDefault     bool       `json:"freeze_default"`
	PauseStatus       string     `json:"pause_status"` // NOT_APPLICABLE, PAUSED, UNPAUSED
	Deleted           bool       `json:"deleted"`
	CustomFees        CustomFees `json:"custom_fees"`
	CreatedTimestamp  string     `json:"created_timestamp"`
}

type Nft struct {
	AccountID        string  `json:"account_id"`
	TokenID          string  `json:"token_id"`
	SerialNumber     int64   `json:"serial_number"`
	Metadata         string  `json:"metadata"` // base64
	Deleted          bool    `json:"deleted"`
	Spender          *string `json:"spender"`
	CreatedTimestamp string  `json:"created_timestamp"`
}

type Nfts struct {
	Nfts  []Nft `json:"nfts"`
	Links Links `json:"links"`
}

type ChunkInfo struct {
	InitialTransactionID any   `json:"initial_transaction_id"`
	Number               int32 `json:"number"`
	Total                int32 `json:"total"`
}

type TopicMessage struct {
	ConsensusTimestamp string     `json:"consensus_timestamp"`
	Message            string     `json:"message"` // base64
	PayerAccountID     string     `json:"payer_account_id"`
	RunningHash        string     `json:"running_hash"` // base64
	RunningHashVersion int64      `json:"running_hash_version"`
	SequenceNumber     int64      `json:"sequence_number"`
	TopicID            string     `json:"topic_id"`
	ChunkInfo          *ChunkInfo `json:"chunk_info"`
}

type TopicMessages struct {
	Messages []TopicMessage `json:"messages"`
	Links    Links          `json:"links"`
}

type Topic struct {
	TopicID          string `json:"topic_id"`
	Memo             string `json:"memo"`
	AdminKey         *Key   `json:"admin_key"`
	SubmitKey        *Key   `json:"submit_key"`
	Deleted          bool   `json:"deleted"`
	CreatedTimestamp string `json:"created_timestamp"`
}

type ScheduleSignature struct {
	ConsensusTimestamp string `json:"consensus_timestamp"`
	PublicKeyPrefix    string `json:"public_key_prefix"` // base64
	Signature          string `json:"signature"`         // base64
	Type               string `json:"type"`              // ED25519, ECDSA_SECP256K1
}

type Schedule struct {
	ScheduleID         string              `json:"schedule_id"`
	CreatorAccountID   string              `json:"creator_account_id"`
	PayerAccountID     string              `json:"payer_account_id"`
	AdminKey           *Key                `json:"admin_key"`
	Memo               string              `json:"memo"`
	ConsensusTimestamp string              `json:"consensus_timestamp"`
	ExecutedTimestamp  *string             `json:"executed_timestamp"`
	ExpirationTime     *string             `json:"expiration_time"`
	WaitForExpiry      bool                `json:"wait_for_expiry"`
	Deleted            bool                `json:"deleted"`
	TransactionBody    string              `json:"transaction_body"` // base64
	Signatures         []ScheduleSignature `json:"signatures"`
}

type TokenAirdrop struct {
	Amount       int64    `json:"amount"`
	ReceiverID   string   `json:"receiver_id"`
	SenderID     string   `json:"sender_id"`
	SerialNumber *int64   `json:"serial_number"`
	TokenID      string   `json:"token_id"`
	Timestamp    Timespan `json:"timestamp"`
}

type Timespan struct {
	From string  `json:"from"`
	To   *string `json:"to"`
}

type TokenAirdrops struct {
	Airdrops []TokenAirdrop `json:"airdrops"`
	Links    Links          `json:"links"`
}

type Transfer struct {
	Account    string `json:"account"`
	Amount     int64  `json:"amount"`
	IsApproval bool   `json:"is_approval"`
}

type TokenTransfer struct {
	TokenID    string `json:"token_id"`
	Account    string `json:"account"`
	Amount     int64  `json:"amount"`
	IsApproval bool   `json:"is_approval"`
}

type NftTransfer struct {
	TokenID           string  `json:"token_id"`
	SerialNumber      int64   `json:"serial_number"`
	SenderAccountID   *string `json:"sender_account_id"`
	ReceiverAccountID *string `json:"receiver_account_id"`
	IsApproval        bool    `json:"is_approval"`
}

type AssessedCustomFee struct {
	Amount                   int64    `json:"amount"`
	CollectorAccountID       string   `json:"collector_account_id"`
	EffectivePayerAccountIDs []string `json:"effective_payer_account_ids"`
	TokenID                  *string  `json:"token_id"`
}

type Transaction struct {
	TransactionID       string              `json:"transaction_id"` // 0.0.x-secs-nanos
	Name                string              `json:"name"`           // CRYPTOTRANSFER, TOKENCREATION, ...
	Result              string              `json:"result"`
	ConsensusTimestamp  string              `json:"consensus_timestamp"`
	ValidStartTimestamp string              `json:"valid_start_timestamp"`
	ChargedTxFee        int64               `json:"charged_tx_fee"`
	MaxFee              string              `json:"max_fee"`
	MemoBase64          string              `json:"memo_base64"`
	Node                *string             `json:"node"`
	Nonce               int32               `json:"nonce"`
	Scheduled           bool                `json:"scheduled"`
	EntityID            *string             `json:"entity_id"`
	Transfers           []Transfer          `json:"transfers"`
	TokenTransfers      []TokenTransfer     `json:"token_transfers"`
	NftTransfers        []NftTransfer       `json:"nft_transfers"`
	AssessedCustomFees  []AssessedCustomFee `json:"assessed_custom_fees"`
}

type Transactions struct {
	Transactions []Transaction `json:"transactions"`
	Links        Links         `json:"links"`
}

type ErrorMessage struct {
	Message string `json:"message"`
}

type ErrorStatus struct {
	Messages []ErrorMessage `json:"messages"`
}

type Error struct {
	Status ErrorStatus `json:"_status"`
}
