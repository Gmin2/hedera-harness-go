package mock

import (
	"encoding/hex"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"google.golang.org/protobuf/proto"
)

// signature is one verified signature from a SignatureMap.
type signature struct {
	key *services.Key // always a primitive ed25519 or ecdsa key
	sig []byte
	evm string // hex evm address for ecdsa keys, empty for ed25519
}

// sigSet holds verified signatures keyed by primitiveID.
type sigSet map[string]signature

func protoKey(pk hiero.PublicKey) *services.Key {
	raw, err := hiero.KeyToBytes(pk)
	if err != nil {
		return nil
	}
	k := new(services.Key)
	if proto.Unmarshal(raw, k) != nil {
		return nil
	}
	return k
}

// primitiveID names a simple key, ok is false for lists and unsupported kinds.
func primitiveID(k *services.Key) (string, bool) {
	switch v := k.GetKey().(type) {
	case *services.Key_Ed25519:
		return "ed25519:" + hex.EncodeToString(v.Ed25519), true
	case *services.Key_ECDSASecp256K1:
		return "ecdsa:" + hex.EncodeToString(v.ECDSASecp256K1), true
	}
	return "", false
}

// verifySignatures checks every pair against the body bytes and keeps the ones
// that verify. Invalid signatures are dropped rather than failing the whole
// map, the keys they claim simply do not count as signed.
func verifySignatures(body []byte, m *services.SignatureMap) sigSet {
	set := sigSet{}
	for _, pair := range m.GetSigPair() {
		var (
			pk  hiero.PublicKey
			err error
			sig []byte
		)
		switch v := pair.GetSignature().(type) {
		case *services.SignaturePair_Ed25519:
			pk, err = hiero.PublicKeyFromBytesEd25519(pair.PubKeyPrefix)
			sig = v.Ed25519
		case *services.SignaturePair_ECDSASecp256K1:
			pk, err = hiero.PublicKeyFromBytesECDSA(pair.PubKeyPrefix)
			sig = v.ECDSASecp256K1
		default:
			continue
		}
		if err != nil || !pk.VerifySignedMessage(body, sig) {
			continue
		}

		s := signature{sig: sig}
		if _, ok := pair.GetSignature().(*services.SignaturePair_ECDSASecp256K1); ok {
			s.key = &services.Key{Key: &services.Key_ECDSASecp256K1{ECDSASecp256K1: pk.BytesRaw()}}
			s.evm = pk.ToEvmAddress()
		} else {
			s.key = &services.Key{Key: &services.Key_Ed25519{Ed25519: pk.BytesRaw()}}
		}
		id, _ := primitiveID(s.key)
		set[id] = s
	}
	return set
}

// satisfies reports whether the signatures meet the key structure: every
// member of a key list, or threshold members of a threshold key, nested.
func (s sigSet) satisfies(k *services.Key) bool {
	switch v := k.GetKey().(type) {
	case *services.Key_Ed25519, *services.Key_ECDSASecp256K1:
		id, _ := primitiveID(k)
		_, ok := s[id]
		return ok
	case *services.Key_KeyList:
		keys := v.KeyList.GetKeys()
		if len(keys) == 0 {
			return false
		}
		for _, member := range keys {
			if !s.satisfies(member) {
				return false
			}
		}
		return true
	case *services.Key_ThresholdKey:
		need := int(v.ThresholdKey.GetThreshold())
		keys := v.ThresholdKey.GetKeys().GetKeys()
		if need == 0 || need > len(keys) {
			return false
		}
		signed := 0
		for _, member := range keys {
			if s.satisfies(member) {
				signed++
			}
		}
		return signed >= need
	}
	return false
}

func (s sigSet) signedByEvm(addr string) bool {
	for _, sig := range s {
		if sig.evm != "" && sig.evm == addr {
			return true
		}
	}
	return false
}

// collectPrimitives adds the ids of every simple key inside k to out.
func collectPrimitives(k *services.Key, out map[string]bool) {
	switch v := k.GetKey().(type) {
	case *services.Key_Ed25519, *services.Key_ECDSASecp256K1:
		id, _ := primitiveID(k)
		out[id] = true
	case *services.Key_KeyList:
		for _, member := range v.KeyList.GetKeys() {
			collectPrimitives(member, out)
		}
	case *services.Key_ThresholdKey:
		for _, member := range v.ThresholdKey.GetKeys().GetKeys() {
			collectPrimitives(member, out)
		}
	}
}

func validKey(k *services.Key) bool {
	switch v := k.GetKey().(type) {
	case *services.Key_Ed25519:
		return len(v.Ed25519) == 32
	case *services.Key_ECDSASecp256K1:
		return len(v.ECDSASecp256K1) == 33
	case *services.Key_KeyList:
		for _, member := range v.KeyList.GetKeys() {
			if !validKey(member) {
				return false
			}
		}
		return true
	case *services.Key_ThresholdKey:
		keys := v.ThresholdKey.GetKeys().GetKeys()
		if int(v.ThresholdKey.GetThreshold()) > len(keys) || v.ThresholdKey.GetThreshold() == 0 {
			return false
		}
		for _, member := range keys {
			if !validKey(member) {
				return false
			}
		}
		return true
	}
	return false
}

func mirrorKey(k *services.Key) *mirror.Key {
	switch v := k.GetKey().(type) {
	case nil:
		return nil
	case *services.Key_Ed25519:
		return &mirror.Key{Type: "ED25519", Key: hex.EncodeToString(v.Ed25519)}
	case *services.Key_ECDSASecp256K1:
		return &mirror.Key{Type: "ECDSA_SECP256K1", Key: hex.EncodeToString(v.ECDSASecp256K1)}
	}
	raw, _ := proto.Marshal(k)
	return &mirror.Key{Type: "ProtobufEncoded", Key: hex.EncodeToString(raw)}
}
