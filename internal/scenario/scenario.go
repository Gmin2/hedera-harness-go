// Package scenario loads hh scenario files.
//
//	name: kyc gated token
//	actors:
//	  treasury: { key: ecdsa, hbar: 20 }
//	  alice:    { key: ed25519, hbar: 5 }
//	steps:
//	  - token.create: { as: gold, name: Gold, symbol: GLD, treasury: treasury, kyc_key: treasury }
//	  - token.associate: { account: alice, tokens: [gold] }
//	assert:
//	  - token.balance: { account: alice, token: gold, equals: 0 }
package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Scenario struct {
	Name        string
	Description string
	Network     string
	Path        string
	Actors      []Actor
	Steps       []Step
	Asserts     []Step
}

type Actor struct {
	Name                string   `yaml:"-"`
	Line                int      `yaml:"-"`
	Key                 string   `yaml:"key"`  // ecdsa (default) or ed25519
	Hbar                *float64 `yaml:"hbar"` // initial balance, default 10
	MaxAutoAssociations *int32   `yaml:"max_auto_associations"`
	ReceiverSigRequired bool     `yaml:"receiver_sig_required"`
	Memo                string   `yaml:"memo"`
	Threshold           uint     `yaml:"threshold"` // with keys: a threshold key account
	Keys                []string `yaml:"keys"`      // member actor names
	// an account that already exists
	ID         string `yaml:"id"`
	PrivateKey string `yaml:"private_key"` // supports ${ENV_VAR}
}

// Step is one entry of steps or assert: a single key map of op name to params.
type Step struct {
	Op     string
	Line   int
	Params *yaml.Node
}

// Decode strictly decodes the params into v, so typos in field names fail.
func (s Step) Decode(v any) error {
	if s.Params == nil || s.Params.Kind == 0 {
		return nil
	}
	if s.Params.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s expects a map of fields", s.Line, s.Op)
	}
	raw, err := yaml.Marshal(s.Params)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		// the node was re-marshalled, so inner line numbers mean nothing
		msg := innerLine.ReplaceAllString(cleanYAMLError(err), "")
		msg = strings.Join(strings.Fields(strings.TrimPrefix(msg, "unmarshal errors:")), " ")
		return fmt.Errorf("line %d: %s: %s", s.Line, s.Op, msg)
	}
	return nil
}

var innerLine = regexp.MustCompile(`line \d+: `)

type file struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Network     string    `yaml:"network"`
	Actors      yaml.Node `yaml:"actors"`
	Steps       yaml.Node `yaml:"steps"`
	Assert      yaml.Node `yaml:"assert"`
}

func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sc.Path = path
	if sc.Name == "" {
		sc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return sc, nil
}

func Parse(data []byte) (*Scenario, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, errors.New(cleanYAMLError(err))
	}
	sc := &Scenario{Name: f.Name, Description: f.Description, Network: f.Network}

	actors, err := parseActors(&f.Actors)
	if err != nil {
		return nil, err
	}
	sc.Actors = actors
	if sc.Steps, err = parseSteps(&f.Steps, "steps"); err != nil {
		return nil, err
	}
	if sc.Asserts, err = parseSteps(&f.Assert, "assert"); err != nil {
		return nil, err
	}
	if len(sc.Steps) == 0 && len(sc.Asserts) == 0 {
		return nil, errors.New("scenario has no steps and no assert")
	}
	return sc, nil
}

func parseActors(n *yaml.Node) ([]Actor, error) {
	if n.Kind == 0 {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: actors must be a map of name to settings", n.Line)
	}
	var out []Actor
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name, body := n.Content[i].Value, n.Content[i+1]
		if seen[name] {
			return nil, fmt.Errorf("line %d: actor %q declared twice", n.Content[i].Line, name)
		}
		seen[name] = true
		a := Actor{Name: name, Line: n.Content[i].Line}
		if body.Kind != 0 && !(body.Kind == yaml.ScalarNode && body.Value == "") {
			st := Step{Op: "actor " + name, Line: a.Line, Params: body}
			if err := st.Decode(&a); err != nil {
				return nil, err
			}
		}
		a.PrivateKey = os.ExpandEnv(a.PrivateKey)
		a.ID = os.ExpandEnv(a.ID)
		out = append(out, a)
	}
	return out, nil
}

func parseSteps(n *yaml.Node, section string) ([]Step, error) {
	if n.Kind == 0 {
		return nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("line %d: %s must be a list", n.Line, section)
	}
	var out []Step
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode || len(item.Content) != 2 {
			return nil, fmt.Errorf("line %d: each %s entry is one op, like `- token.create: {...}`", item.Line, section)
		}
		out = append(out, Step{
			Op:     item.Content[0].Value,
			Line:   item.Content[0].Line,
			Params: item.Content[1],
		})
	}
	return out, nil
}

// Discover finds scenario files under dir, sorted by path.
func Discover(dir string) ([]*Scenario, error) {
	var out []*Scenario
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		sc, err := Load(path)
		if err != nil {
			return nil // not a scenario, skip
		}
		out = append(out, sc)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func cleanYAMLError(err error) string {
	return strings.TrimPrefix(err.Error(), "yaml: ")
}
