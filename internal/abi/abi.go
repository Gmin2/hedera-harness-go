// Package abi encodes the simple solidity calls hh assertions make: a
// function signature with static arguments, and one decoded return value.
package abi

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// Signature is a parsed "balanceOf(address)".
type Signature struct {
	Name   string
	Inputs []string
}

func ParseSignature(sig string) (Signature, error) {
	sig = strings.ReplaceAll(sig, " ", "")
	open := strings.IndexByte(sig, '(')
	if open <= 0 || !strings.HasSuffix(sig, ")") {
		return Signature{}, fmt.Errorf("function %q should look like name(type,type)", sig)
	}
	s := Signature{Name: sig[:open]}
	if inner := sig[open+1 : len(sig)-1]; inner != "" {
		s.Inputs = strings.Split(inner, ",")
	}
	return s, nil
}

func (s Signature) String() string { return s.Name + "(" + strings.Join(s.Inputs, ",") + ")" }

// Selector is the first four bytes of the keccak256 of the signature.
func (s Signature) Selector() []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(s.String()))
	return h.Sum(nil)[:4]
}

// Encode builds call data for static arguments. Addresses must already be
// 0x hex, numbers are decimal strings.
func (s Signature) Encode(args []string) (string, error) {
	if len(args) != len(s.Inputs) {
		return "", fmt.Errorf("%s takes %d arguments, got %d", s, len(s.Inputs), len(args))
	}
	out := append([]byte(nil), s.Selector()...)
	for i, typ := range s.Inputs {
		word, err := encodeWord(typ, args[i])
		if err != nil {
			return "", fmt.Errorf("argument %d: %w", i+1, err)
		}
		out = append(out, word...)
	}
	return "0x" + hex.EncodeToString(out), nil
}

func encodeWord(typ, v string) ([]byte, error) {
	word := make([]byte, 32)
	switch {
	case typ == "address":
		b, err := hex.DecodeString(strings.TrimPrefix(v, "0x"))
		if err != nil || len(b) != 20 {
			return nil, fmt.Errorf("%q is not a 20 byte address", v)
		}
		copy(word[12:], b)
	case typ == "bool":
		if v == "true" {
			word[31] = 1
		} else if v != "false" {
			return nil, fmt.Errorf("%q is not a bool", v)
		}
	case strings.HasPrefix(typ, "uint"), strings.HasPrefix(typ, "int"):
		n, err := ParseNumber(v)
		if err != nil {
			return nil, err
		}
		if n.Sign() < 0 {
			// two's complement over 256 bits
			n = new(big.Int).Add(n, new(big.Int).Lsh(big.NewInt(1), 256))
		}
		n.FillBytes(word)
	default:
		return nil, fmt.Errorf("argument type %s is not supported yet", typ)
	}
	return word, nil
}

// Decode reads one return value of typ from hex result data.
func Decode(typ, result string) (string, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(result, "0x"))
	if err != nil {
		return "", fmt.Errorf("result is not hex: %w", err)
	}
	if len(b) < 32 {
		return "", errors.New("empty result, the contract may not exist at that address or the call reverted")
	}
	switch {
	case typ == "string":
		off := new(big.Int).SetBytes(b[:32]).Int64()
		if off+32 > int64(len(b)) {
			return "", errors.New("bad string offset")
		}
		n := new(big.Int).SetBytes(b[off : off+32]).Int64()
		if off+32+n > int64(len(b)) {
			return "", errors.New("bad string length")
		}
		return string(b[off+32 : off+32+n]), nil
	case typ == "bool":
		return fmt.Sprint(b[31] == 1), nil
	case typ == "address":
		return "0x" + hex.EncodeToString(b[12:32]), nil
	case strings.HasPrefix(typ, "uint"):
		return new(big.Int).SetBytes(b[:32]).String(), nil
	case strings.HasPrefix(typ, "int"):
		n := new(big.Int).SetBytes(b[:32])
		if b[0]&0x80 != 0 {
			n.Sub(n, new(big.Int).Lsh(big.NewInt(1), 256))
		}
		return n.String(), nil
	case typ == "bytes32":
		return "0x" + hex.EncodeToString(b[:32]), nil
	}
	return "", fmt.Errorf("return type %s is not supported yet", typ)
}

// ParseNumber accepts plain integers and exponent forms like 10000e18.
func ParseNumber(v string) (*big.Int, error) {
	v = strings.ReplaceAll(strings.TrimSpace(v), "_", "")
	if n, ok := new(big.Int).SetString(v, 10); ok {
		return n, nil
	}
	f, _, err := big.ParseFloat(v, 10, 512, big.ToZero)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number", v)
	}
	n, acc := f.Int(nil)
	if acc != big.Exact {
		return nil, fmt.Errorf("%q is not a whole number", v)
	}
	return n, nil
}
