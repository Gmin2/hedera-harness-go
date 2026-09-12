// Package keys parses private keys without guessing.
//
// hiero.PrivateKeyFromString tries ed25519 first, so a raw ecdsa hex key
// silently becomes the wrong key and every transaction fails with
// INVALID_SIGNATURE. Here a raw key must carry its type, either from the
// caller, a "ecdsa:"/"ed25519:" prefix, or a 0x prefix (evm style, ecdsa).
package keys

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

const (
	ED25519 = "ed25519"
	ECDSA   = "ecdsa"
)

// ErrAmbiguous means a raw 32 byte hex key was given without a type.
var ErrAmbiguous = errors.New("raw 32 byte key could be ed25519 or ecdsa, set the key type")

const ed25519DerPrefix = "302e020100300506032b657004220420"

// Parse reads a private key. kind may be "", "ed25519" or "ecdsa".
func Parse(s, kind string) (hiero.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if k, rest, ok := strings.Cut(s, ":"); ok && (k == ED25519 || k == ECDSA) {
		kind, s = k, rest
	}
	kind = strings.ToLower(kind)

	if strings.HasPrefix(s, "0x") {
		if kind == "" {
			kind = ECDSA
		}
		s = s[2:]
	}
	if _, err := hex.DecodeString(s); err != nil {
		return hiero.PrivateKey{}, fmt.Errorf("key is not hex: %w", err)
	}

	isDer := len(s) > 64 && strings.HasPrefix(s, "30")
	switch {
	case isDer && kind == "":
		return hiero.PrivateKeyFromStringDer(s)
	case kind == ED25519:
		return hiero.PrivateKeyFromStringEd25519(s)
	case kind == ECDSA:
		return hiero.PrivateKeyFromStringECDSA(s)
	case kind != "":
		return hiero.PrivateKey{}, fmt.Errorf("unknown key type %q", kind)
	}
	return hiero.PrivateKey{}, ErrAmbiguous
}

// Kind reports ed25519 or ecdsa for a parsed key.
func Kind(k hiero.PrivateKey) string {
	if strings.HasPrefix(k.StringDer(), ed25519DerPrefix) {
		return ED25519
	}
	return ECDSA
}

// PublicKind reports ed25519 or ecdsa for a public key.
func PublicKind(k hiero.PublicKey) string {
	if strings.HasPrefix(k.StringDer(), "302a300506032b6570") {
		return ED25519
	}
	return ECDSA
}

// Generate makes a new key of the given kind.
func Generate(kind string) (hiero.PrivateKey, error) {
	switch strings.ToLower(kind) {
	case "", ECDSA:
		return hiero.PrivateKeyGenerateEcdsa()
	case ED25519:
		return hiero.PrivateKeyGenerateEd25519()
	}
	return hiero.PrivateKey{}, fmt.Errorf("unknown key type %q", kind)
}
