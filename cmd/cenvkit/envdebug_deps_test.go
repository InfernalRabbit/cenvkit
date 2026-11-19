package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// depsFixture: A depends on B, C references A, the web env_file references A
// and the web service interpolates A in YAML.
func depsFixture(t *testing.T) string {
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
	write(".cenvkit.envchain", ".env\n")
	write(".env", "B=1\nA=${B}\nC=${A}\n")
	write("docker-compose.yml", "services:\n  web:\n    image: nginx\n    env_file:\n      - web/.web.env\n    environment:\n      EXTRA: ${A}\n")
	write("web/.web.env", "WEB=${A}\n")
	return dir
}

func TestEnvDebug_DepsHuman(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := depsFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "deps", "--var", "A")
	if err != nil {
		t.Fatalf("deps failed: %v (out=%s)", err, out)
	}
	for _, want := range []string{"defined in:", ".env (layer1)", "depends on:", "B (depth 1", "needed by:", "C", "consumed by:", "service web"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestEnvDebug_DepsJSON(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := depsFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--json", "--show", "deps", "--var", "A")
	if err != nil {
		t.Fatalf("json deps failed: %v", err)
	}
	var doc struct {
		Deps *struct {
			Root        string                           `json:"root"`
			Definitions []struct{ File string }          `json:"definitions"`
			DependsOn   []struct{ Var string }           `json:"depends_on"`
			NeededBy    []struct{ Key string }           `json:"needed_by"`
			Consumers   []struct{ Service, Kind string } `json:"consumers"`
		} `json:"deps"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if doc.Deps == nil || doc.Deps.Root != "A" {
		t.Fatalf("deps section missing: %s", out)
	}
	if len(doc.Deps.Definitions) != 1 {
		t.Fatalf("definitions: %+v", doc.Deps.Definitions)
	}
	if len(doc.Deps.DependsOn) != 1 || doc.Deps.DependsOn[0].Var != "B" {
		t.Fatalf("depends_on: %+v", doc.Deps.DependsOn)
	}
	// C (chain key) and WEB (env_file key) both reference A.
	keys := map[string]bool{}
	for _, e := range doc.Deps.NeededBy {
		keys[e.Key] = true
	}
	if !keys["C"] || !keys["WEB"] {
		t.Fatalf("needed_by must contain C and WEB: %+v", doc.Deps.NeededBy)
	}
	if len(doc.Deps.Consumers) == 0 {
		t.Fatalf("consumers empty")
	}
}

func TestEnvDebug_DepsRequiresVar(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := depsFixture(t)
	_, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "deps")
	wantExit2(t, err, "requires --var")
}
