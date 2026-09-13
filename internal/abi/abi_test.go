package abi

import "testing"

func TestEncodeDecode(t *testing.T) {
	sig, err := ParseSignature("balanceOf(address)")
	if err != nil {
		t.Fatal(err)
	}
	// well known selector of erc20 balanceOf
	if got := sig.Selector(); string(got) != "\x70\xa0\x82\x31" {
		t.Fatalf("selector %x", got)
	}
	data, err := sig.Encode([]string{"0x00000000000000000000000000000000000004d2"})
	if err != nil || data != "0x70a0823100000000000000000000000000000000000000000000000000000000000004d2" {
		t.Fatalf("encode %s %v", data, err)
	}

	str := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000003" +
		"48544b0000000000000000000000000000000000000000000000000000000000"
	if v, err := Decode("string", str); err != nil || v != "HTK" {
		t.Fatalf("string %q %v", v, err)
	}
	supply := "0x00000000000000000000000000000000000000000000021e19e0c9bab2400000"
	if v, err := Decode("uint256", supply); err != nil || v != "10000000000000000000000" {
		t.Fatalf("uint %q %v", v, err)
	}
	if n, err := ParseNumber("10000e18"); err != nil || n.String() != "10000000000000000000000" {
		t.Fatalf("number %v %v", n, err)
	}
	if _, err := Decode("uint256", "0x"); err == nil {
		t.Fatal("empty result should explain itself")
	}
}
