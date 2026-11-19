package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureSelectorDefault builds a project whose COMPOSE_FILE uses the selector
// token while NOTHING in the chain or the shell defines CENVKIT_ENV, i.e. the
// selector resolves to its "dev" default. Regression guard: the resolved default
// must be exported into the compose child env and into the debug/engine env, or
// the documented COMPOSE_FILE pattern expands to "compose..yml".
func fixtureSelectorDefault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".cenvkit.envchain", ".env\n")
	write(".env", "COMPOSE_FILE=compose."+"${CENVKIT_ENV}.yml\n")
	write("compose.dev.yml", "services:\n  web:\n    image: nginx\n")
	return dir
}

// TestSelectorDefault_EnvExpands: cenvkit env --format json must expand the
// selector token to the effective default tier, not to an empty string.
func TestSelectorDefault_EnvExpands(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "") // ensure the shell does not mask the default
	dir := fixtureSelectorDefault(t)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--project-dir", dir, "env", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("env: %v (out=%s)", err, buf.String())
	}
	if !strings.Contains(buf.String(), "compose.dev.yml") {
		t.Fatalf("selector default not applied; output=%s", buf.String())
	}
	if strings.Contains(buf.String(), "compose..yml") {
		t.Fatalf("selector expanded to empty (compose..yml); output=%s", buf.String())
	}
}

// TestSelectorDefault_EnvDebugLoads: the in-process debug engine must see the same
// effective selector, otherwise COMPOSE_FILE cannot be resolved.
func TestSelectorDefault_EnvDebugLoads(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := fixtureSelectorDefault(t)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--project-dir", dir, "env-debug", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("env-debug: %v (out=%s)", err, buf.String())
	}
	if !strings.Contains(buf.String(), "compose.dev.yml") {
		t.Fatalf("debug interpolation did not resolve the selector default; out=%s", buf.String())
	}
}
