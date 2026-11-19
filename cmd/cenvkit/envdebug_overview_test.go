package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overviewFixture: the chain includes a service env_file, so it plays both roles.
func overviewFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".cenvkit.envchain", ".env\nsvc/.svc.env\n")
	write(".env", "B=1\nA=${B}\n")
	write("svc/.svc.env", "S=${A}\n")
	write("docker-compose.yml", "services:\n  svc:\n    image: nginx\n    env_file:\n      - svc/.svc.env\n")
	return dir
}

func TestOverview_ChainOnlyVarListsReferences(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := overviewFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "overview", "--var", "B")
	if err != nil {
		t.Fatalf("overview failed: %v", err)
	}
	for _, want := range []string{"not declared in any service env_file", "referenced by", "A", ".svc.env:S"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestOverview_ChainFileAnnotation(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := overviewFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "overview", "--var", "S")
	if err != nil {
		t.Fatalf("overview failed: %v", err)
	}
	if !strings.Contains(out, "also in COMPOSE_ENV_FILES") {
		t.Fatalf("runtime file that is also in the chain must be annotated:\n%s", out)
	}
}

func TestOverview_VarJSONHasNoRefs(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := overviewFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--json", "--show", "overview", "--var", "B")
	if err != nil {
		t.Fatalf("json overview failed: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := doc["refs"]; ok {
		t.Fatalf("refs are internal and must not leak:\n%s", out)
	}
	if _, ok := doc["layers"]; !ok || len(doc) != 1 {
		t.Fatalf("--show overview must emit only layers, got %v", doc)
	}
}
