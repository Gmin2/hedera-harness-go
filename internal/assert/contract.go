package assert

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"

	"github.com/Gmin2/hedera-harness-go/internal/abi"
	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/network"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
)

// contractCall reads a deployed contract through the mirror node, so a
// judge can check what an agent deployed, not only what it wrote.
type contractCall struct {
	// Contract is a hardhat deployment name, a 0x address or a 0.0.x id.
	Contract string   `yaml:"contract"`
	Function string   `yaml:"function"` // eg balanceOf(address)
	Args     []string `yaml:"args"`     // actor names, 0.0.x ids, 0x addresses or numbers
	Returns  string   `yaml:"returns"`  // uint256 by default, or string, bool, address, int256, bytes32
	Equals   *string  `yaml:"equals"`
	// Deployments is where hardhat-deploy writes addresses.
	Deployments string `yaml:"deployments"`
}

var hexAddress = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

func (c *contractCall) returns() string {
	if c.Returns == "" {
		return "uint256"
	}
	return c.Returns
}

func (c *contractCall) Title() string { return c.Contract + "." + c.Function }

func (c *contractCall) Expected() string {
	if c.Equals == nil {
		return ""
	}
	return "= " + *c.Equals
}

func (c *contractCall) Resolve(env *ops.Env) error {
	if c.Contract == "" {
		return fmt.Errorf("contract.call needs contract")
	}
	sig, err := abi.ParseSignature(c.Function)
	if err != nil {
		return err
	}
	if len(c.Args) != len(sig.Inputs) {
		return fmt.Errorf("%s takes %d args, got %d", sig, len(sig.Inputs), len(c.Args))
	}
	if c.Equals == nil {
		return fmt.Errorf("contract.call needs equals")
	}
	return nil
}

func (c *contractCall) Observe(ctx context.Context, env *ops.Env, m *mirror.Client) (Observation, error) {
	if env.Target != nil && env.Target.Mode == network.Mock {
		return Observation{}, fmt.Errorf("the mock network has no evm, run contract checks on local or testnet")
	}
	to, err := c.address(env)
	if err != nil {
		return Observation{}, err
	}
	sig, _ := abi.ParseSignature(c.Function)
	var args []string
	for _, a := range c.Args {
		args = append(args, argValue(env, a))
	}
	data, err := sig.Encode(args)
	if err != nil {
		return Observation{}, err
	}
	result, src, err := m.ContractCall(ctx, to, data)
	if err != nil {
		return Observation{Source: src}, err
	}
	got, err := abi.Decode(c.returns(), result)
	if err != nil {
		return Observation{Source: src}, err
	}
	return Observation{Actual: got, OK: sameValue(c.returns(), got, *c.Equals), Source: src}, nil
}

// address finds the contract, reading hardhat-deploy output for a name.
func (c *contractCall) address(env *ops.Env) (string, error) {
	if hexAddress.MatchString(c.Contract) {
		return strings.ToLower(c.Contract), nil
	}
	if id, err := hiero.ContractIDFromString(c.Contract); err == nil {
		return "0x" + id.ToEvmAddress(), nil
	}
	dir := c.Deployments
	if dir == "" {
		dir = filepath.Join("packages", "hardhat", "deployments")
	}
	netDir := "hederaTestnet"
	if env.Target != nil && env.Target.Mode == network.Local {
		netDir = "localhost"
	}
	path := filepath.Join(dir, netDir, c.Contract+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no deployment for %s at %s, deploy it first", c.Contract, path)
	}
	var dep struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(raw, &dep); err != nil || !hexAddress.MatchString(dep.Address) {
		return "", fmt.Errorf("%s has no address", path)
	}
	return strings.ToLower(dep.Address), nil
}

// argValue turns scenario names into addresses the contract understands.
func argValue(env *ops.Env, v string) string {
	if a, err := env.Actor(v); err == nil {
		if a.EvmAlias != "" {
			return "0x" + strings.TrimPrefix(a.EvmAlias, "0x")
		}
		return "0x" + a.ID.ToEvmAddress()
	}
	if id, ok := env.Lookup(v); ok && strings.Count(id, ".") == 2 {
		if acct, err := hiero.AccountIDFromString(id); err == nil {
			return "0x" + acct.ToEvmAddress()
		}
	}
	return v
}

func sameValue(typ, got, want string) bool {
	if strings.HasPrefix(typ, "uint") || strings.HasPrefix(typ, "int") {
		g, err1 := abi.ParseNumber(got)
		w, err2 := abi.ParseNumber(want)
		return err1 == nil && err2 == nil && g.Cmp(w) == 0
	}
	if typ == "address" || typ == "bytes32" {
		return strings.EqualFold(got, want)
	}
	return got == want
}

func init() {
	register("contract.call", func() Check { return &contractCall{} })
}
