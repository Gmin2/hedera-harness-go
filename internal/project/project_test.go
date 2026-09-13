package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if c, err := Load(dir); c != nil || err != nil {
		t.Fatalf("missing file should be nil, nil: %v %v", c, err)
	}

	os.WriteFile(filepath.Join(dir, FileName), []byte(`
network: testnet
agent: { model: haiku, max_cost: 1.5, timeout: 90s }
judge:
  scenarios: [a.yaml]
  checks:
    - go vet ./...
    - { name: tests, run: go test ./..., timeout: 5m }
`), 0o644)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Network != "testnet" || c.Agent.Model != "haiku" || c.Agent.MaxCost != 1.5 || c.Agent.Timeout.Duration != 90*time.Second {
		t.Fatalf("config: %+v", c)
	}
	if len(c.Judge.Checks) != 2 || c.Judge.Checks[0].Run != "go vet ./..." || c.Judge.Checks[1].Label() != "tests" || c.Judge.Checks[1].Timeout.Duration != 5*time.Minute {
		t.Fatalf("checks: %+v", c.Judge.Checks)
	}

	os.WriteFile(filepath.Join(dir, FileName), []byte("netwrk: mock\n"), 0o644)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "netwrk") {
		t.Fatalf("typo should fail, got %v", err)
	}

	os.WriteFile(filepath.Join(dir, FileName), nil, 0o644)
	if c, err := Load(dir); err != nil || c == nil {
		t.Fatalf("empty file should load defaults: %v", err)
	}

	os.WriteFile(filepath.Join(dir, FileName), []byte(Template), 0o644)
	if _, err := Load(dir); err != nil {
		t.Fatalf("the init template must load: %v", err)
	}
}
