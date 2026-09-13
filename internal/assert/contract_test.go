package assert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	"github.com/Gmin2/hedera-harness-go/internal/ops"
)

func TestContractCallReadsDeployment(t *testing.T) {
	dir := t.TempDir()
	dep := filepath.Join(dir, "hederaTestnet")
	os.MkdirAll(dep, 0o755)
	os.WriteFile(filepath.Join(dep, "HederaToken.json"), []byte(`{"address":"0x00000000000000000000000000000000000004D2"}`), 0o644)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		// symbol() returns "HTK"
		w.Write([]byte(`{"result":"0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000348544b0000000000000000000000000000000000000000000000000000000000"}`))
	}))
	defer srv.Close()

	want := "HTK"
	c := &contractCall{Contract: "HederaToken", Function: "symbol()", Returns: "string", Equals: &want, Deployments: dir}
	env := ops.NewEnv(nil)
	if err := c.Resolve(env); err != nil {
		t.Fatal(err)
	}
	obs, err := c.Observe(context.Background(), env, mirror.New(srv.URL+"/api/v1"))
	if err != nil || !obs.OK || obs.Actual != "HTK" {
		t.Fatalf("observe %+v %v", obs, err)
	}
	if gotBody["to"] != "0x00000000000000000000000000000000000004d2" || gotBody["data"] != "0x95d89b41" {
		t.Fatalf("request body %v", gotBody)
	}

	missing := &contractCall{Contract: "Nope", Function: "symbol()", Returns: "string", Equals: &want, Deployments: dir}
	if _, err := missing.Observe(context.Background(), env, mirror.New(srv.URL+"/api/v1")); err == nil || !strings.Contains(err.Error(), "deploy it first") {
		t.Fatalf("missing deployment should explain, got %v", err)
	}
}
