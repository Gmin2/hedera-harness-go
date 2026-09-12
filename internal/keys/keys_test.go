package keys

import (
	"errors"
	"testing"
)

// hiero local node genesis and first prefunded ecdsa account, dev only keys
const (
	localGenesisDer = "302e020100300506032b65700422042091132178e72057a1d7528025956fe39b0b847f200ab59b2fdd367017f3087137"
	localEcdsaHex   = "0x7f109a9e3b0d8ecfba9cc23a3614433ce0fa7ddcc80f2a8f10b222179a5a80d6"
)

func TestParse(t *testing.T) {
	k, err := Parse(localGenesisDer, "")
	if err != nil {
		t.Fatal(err)
	}
	if Kind(k) != ED25519 {
		t.Fatalf("genesis der parsed as %s", Kind(k))
	}

	k, err = Parse(localEcdsaHex, "")
	if err != nil {
		t.Fatal(err)
	}
	if Kind(k) != ECDSA {
		t.Fatalf("0x key parsed as %s", Kind(k))
	}
	if PublicKind(k.PublicKey()) != ECDSA {
		t.Fatal("public kind mismatch")
	}

	raw := localEcdsaHex[2:]
	if _, err := Parse(raw, ""); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("raw key without type should be ambiguous, got %v", err)
	}
	k, err = Parse("ecdsa:"+raw, "")
	if err != nil || Kind(k) != ECDSA {
		t.Fatalf("prefixed ecdsa: %v %s", err, Kind(k))
	}
	k, err = Parse(raw, "ed25519")
	if err != nil || Kind(k) != ED25519 {
		t.Fatalf("typed ed25519: %v", err)
	}

	ecdsaDer := mustGen(t, ECDSA).StringDer()
	k, err = Parse(ecdsaDer, "")
	if err != nil || Kind(k) != ECDSA {
		t.Fatalf("ecdsa der: %v %s", err, Kind(k))
	}
}

func mustGen(t *testing.T, kind string) interface{ StringDer() string } {
	k, err := Generate(kind)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
