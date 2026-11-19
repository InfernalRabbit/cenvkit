package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cascadeFixture is envFileFixture plus a key that lives ONLY in a service
// env_file: and is never referenced in YAML — the runtime-only case that used to
// answer "not set".
func cascadeFixture(t *testing.T) string {
	t.Helper()
	dir := envFileFixture(t)
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("web/.web.env", "X=1\nONLY_HERE=top-secret-value\n")
	return dir
}

// wantExit2 asserts the command failed with the usage exit code and a hint.
func wantExit2(t *testing.T, err error, substr string) {
	t.Helper()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("want exit 2, got %v", err)
	}
	if substr != "" && !strings.Contains(ee.Error(), substr) {
		t.Fatalf("message %q does not contain %q", ee.Error(), substr)
	}
}

func TestEnvDebug_CascadeOrder(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "files", "--show", "effective", "--var", "X")
	if err != nil {
		t.Fatalf("cascade failed: %v (out=%s)", err, out)
	}
	filesIdx := strings.Index(out, "interpolation (COMPOSE_ENV_FILES):")
	svcIdx := strings.Index(out, "service web:")
	if filesIdx < 0 || svcIdx < 0 || filesIdx > svcIdx {
		t.Fatalf("both views must render, files before effective; got:\n%s", out)
	}
}

func TestEnvDebug_CascadeHonoursGivenOrder(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "effective", "--show", "files", "--var", "X")
	if err != nil {
		t.Fatalf("cascade failed: %v", err)
	}
	if strings.Index(out, "service web:") > strings.Index(out, "interpolation (COMPOSE_ENV_FILES):") {
		t.Fatalf("order was not preserved:\n%s", out)
	}
}

func TestEnvDebug_VarAloneTracesRuntimeOnly(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--var", "ONLY_HERE")
	if err != nil {
		t.Fatalf("trace failed: %v", err)
	}
	if strings.Contains(out, "not set") {
		t.Fatalf("runtime-only key reported as not set:\n%s", out)
	}
	if !strings.Contains(out, "runtime-only") || !strings.Contains(out, ".web.env") {
		t.Fatalf("trace must explain the env_file origin:\n%s", out)
	}
}

func TestEnvDebug_ServiceAloneEffective(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	// No --show and no --var: --service implies the effective view.
	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--service", "web")
	if err != nil {
		t.Fatalf("effective failed: %v", err)
	}
	if !strings.Contains(out, "service web:") || strings.Contains(out, "service api:") {
		t.Fatalf("service filter not applied:\n%s", out)
	}
}

func TestEnvDebug_EffectiveKeyFilter(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "effective", "--service", "web", "--var", "ONLY_HERE")
	if err != nil {
		t.Fatalf("effective failed: %v", err)
	}
	if !strings.Contains(out, "ONLY_HERE") {
		t.Fatalf("key filter not applied:\n%s", out)
	}
	if strings.Contains(out, "X=") || strings.Contains(out, "X\t") {
		t.Fatalf("other keys leaked through the filter:\n%s", out)
	}
}

func TestEnvDebug_Redact(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--show", "effective", "--service", "web", "--redact")
	if err != nil {
		t.Fatalf("redact failed: %v", err)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("expected redaction marker:\n%s", out)
	}
	if strings.Contains(out, "top-secret-value") {
		t.Fatalf("redacted output leaked a value:\n%s", out)
	}
}

func TestEnvDebug_Conflicts(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)
	cases := []struct {
		name string
		args []string
		hint string
	}{
		{"value+view", []string{"--value", "--var", "X", "--show", "trace"}, "cannot be combined"},
		{"value+json", []string{"--value", "--var", "X", "--json"}, "cannot be combined"},
		{"value without var", []string{"--value"}, "requires --var"},
		{"trace without var", []string{"--show", "trace"}, "requires --var"},
		{"service without per-service view", []string{"--service", "web", "--show", "list"}, "needs a per-service view"},
		{"unknown view", []string{"--show", "nope"}, "unknown --show"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--project-dir", dir, "env-debug"}, c.args...)
			_, err := runRoot(t, args...)
			wantExit2(t, err, c.hint)
		})
	}
}

func TestEnvDebug_JSONSections(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--json", "--show", "files")
	if err != nil {
		t.Fatalf("json failed: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "files,services" {
		t.Fatalf("--show files must emit only files+services, got %v", keys)
	}
}

func TestEnvDebug_JSONVarFilter(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := cascadeFixture(t)

	out, err := runRoot(t, "--project-dir", dir, "env-debug", "--json", "--var", "ONLY_HERE")
	if err != nil {
		t.Fatalf("json failed: %v", err)
	}
	var doc struct {
		Vars     map[string]json.RawMessage `json:"vars"`
		Services []struct {
			Service string `json:"service"`
			Entries []struct {
				Key string `json:"key"`
			} `json:"entries"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(doc.Vars) != 0 {
		t.Fatalf("ONLY_HERE is not an interpolation variable, vars should be empty: %v", doc.Vars)
	}
	if len(doc.Services) != 1 || doc.Services[0].Service != "web" {
		t.Fatalf("service filter lost: %+v", doc.Services)
	}
	if len(doc.Services[0].Entries) != 1 || doc.Services[0].Entries[0].Key != "ONLY_HERE" {
		t.Fatalf("entry filter lost: %+v", doc.Services[0].Entries)
	}
}
