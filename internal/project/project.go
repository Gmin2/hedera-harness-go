// Package project reads hh.yaml, the one place a project declares its
// network, scenarios, judges and agent settings. The cli, tui and agent loop
// all start from it, flags only override.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const FileName = "hh.yaml"

type Config struct {
	Path      string   `yaml:"-"`
	Network   string   `yaml:"network"`
	Scenarios []string `yaml:"scenarios"` // files or dirs the tui lists, default "."
	Wallet    Wallet   `yaml:"wallet"`
	Agent     Agent    `yaml:"agent"`
	Judge     Judge    `yaml:"judge"`
}

type Agent struct {
	Model       string   `yaml:"model"`
	MaxCost     float64  `yaml:"max_cost"`
	MaxAttempts int      `yaml:"max_attempts"`
	Timeout     Duration `yaml:"timeout"`
}

type Judge struct {
	Scenarios []string `yaml:"scenarios"`
	Checks    []Check  `yaml:"checks"`
}

// Wallet picks the account that pays and signs. In yaml it is either a kind
// (default, burner) or { kind: burner, fund: 30 }.
type Wallet struct {
	Kind string  `yaml:"kind"`
	Fund float64 `yaml:"fund"`
}

func (w *Wallet) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		w.Kind = n.Value
		return nil
	}
	type plain Wallet
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*w = Wallet(p)
	return nil
}

// Check is a shell command that must exit 0. In yaml it is either a plain
// string or { name, run, timeout }.
type Check struct {
	Name    string   `yaml:"name"`
	Run     string   `yaml:"run"`
	Timeout Duration `yaml:"timeout"`
}

func (c *Check) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		c.Run = n.Value
		return nil
	}
	type plain Check
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*c = Check(p)
	return nil
}

func (c Check) Label() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Run
}

// Duration reads values like 90s or 20m.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %q is not a duration like 90s or 20m", n.Line, n.Value)
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// Load reads hh.yaml from dir. A missing file returns nil and no error.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// an empty file decodes as io.EOF and simply means defaults
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %s", path, strings.TrimPrefix(err.Error(), "yaml: "))
	}
	switch c.Wallet.Kind {
	case "", "default", "burner":
	default:
		return nil, fmt.Errorf("%s: wallet %q must be default or burner (import keys in the tui with ctrl+w)", path, c.Wallet.Kind)
	}
	for i, ch := range c.Judge.Checks {
		if strings.TrimSpace(ch.Run) == "" {
			return nil, fmt.Errorf("%s: judge.checks[%d] has no run command", path, i)
		}
	}
	c.Path = path
	return &c, nil
}

// Template is what hh init writes.
const Template = `# hh project settings. flags override these.
network: mock          # mock, local or testnet
wallet: default        # default operator, or burner: a fresh account funded per run

# where the tui looks for scenarios
scenarios: [scenarios]

agent:
  model: sonnet        # any claude model, haiku is quickest
  max_cost: 2          # usd per prompt, across repair attempts
  max_attempts: 3
  timeout: 20m

# what claude's work must pass after every attempt
judge:
  scenarios:
    - scenarios/first-transfer.yaml
  checks: []
    # - go test ./...
    # - { name: contracts, run: yarn hardhat:test, timeout: 5m }
`
