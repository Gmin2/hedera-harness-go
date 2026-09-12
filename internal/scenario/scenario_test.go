package scenario

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	sc, err := Parse([]byte(`
name: demo
network: testnet
actors:
  treasury: { key: ecdsa, hbar: 20 }
  alice:
steps:
  - token.create: { as: gold, name: Gold, symbol: GLD }
  - token.associate: { account: alice, token: gold }
assert:
  - token.balance: { account: alice, token: gold, equals: 0 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Name != "demo" || sc.Network != "testnet" {
		t.Fatalf("header: %+v", sc)
	}
	if len(sc.Actors) != 2 || sc.Actors[0].Name != "treasury" || sc.Actors[1].Name != "alice" {
		t.Fatalf("actors should keep file order: %+v", sc.Actors)
	}
	if *sc.Actors[0].Hbar != 20 {
		t.Fatalf("hbar: %v", *sc.Actors[0].Hbar)
	}
	if len(sc.Steps) != 2 || sc.Steps[1].Op != "token.associate" || sc.Steps[1].Line != 9 {
		t.Fatalf("steps: %+v", sc.Steps)
	}
}

func TestStrictFields(t *testing.T) {
	sc, err := Parse([]byte(`
steps:
  - hbar.transfer: { from: alice, to: bob, amout: 1 }
`))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		From   string  `yaml:"from"`
		To     string  `yaml:"to"`
		Amount float64 `yaml:"amount"`
	}
	err = sc.Steps[0].Decode(&v)
	if err == nil || !strings.Contains(err.Error(), "amout") || !strings.HasPrefix(err.Error(), "line 3:") {
		t.Fatalf("typo should be reported with the step line, got %v", err)
	}
}

func TestBadShapes(t *testing.T) {
	cases := map[string]string{
		"steps: {a: 1}":                        "steps must be a list",
		"steps:\n  - a: 1\n    b: 2":           "each steps entry is one op",
		"actors: [a, b]\nsteps:\n  - x: {}":    "actors must be a map",
		"name: nothing":                        "no steps and no assert",
		"actors:\n  a: {}\n  a: {}\nsteps: []": "",
	}
	for in, want := range cases {
		_, err := Parse([]byte(in))
		if err == nil {
			t.Errorf("%q: expected an error", in)
			continue
		}
		if want != "" && !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
}
