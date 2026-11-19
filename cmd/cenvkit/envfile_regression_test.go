package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envFileFixture builds a project whose only ${X} reference is satisfied by a
// service env_file: — i.e. a real interpolation gap unless an --env-file layer
// supplies X. Mirrors `docker compose --env-file`.
func envFileFixture(t *testing.T) string {
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
	write("docker-compose.yml", "services:\n  web:\n    image: nginx\n    env_file:\n      - web/.web.env\n    ports:\n      - \"${X:-0}:80\"\n")
	write("web/.web.env", "X=1\n")
	write("extra.env", "X=42\n")
	return dir
}

// runRoot executes cenvkit with args, returning stdout and the error.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// TestEnvFile_ClosesFalseGap: without --env-file the port falls back (a gap,
// exit 1); with it, the file participates in interpolation and the gap is gone.
func TestEnvFile_ClosesFalseGap(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := envFileFixture(t)

	_, err := runRoot(t, "--project-dir", dir, "gap-report")
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("without --env-file: want exit 1 from gap-report, got %v", err)
	}

	out, err := runRoot(t, "--project-dir", dir, "gap-report", "--env-file", "extra.env")
	if err != nil {
		t.Fatalf("with --env-file: gap-report failed: %v (out=%s)", err, out)
	}

	out, err = runRoot(t, "--project-dir", dir, "env", "--env-file", "extra.env", "--format", "json")
	if err != nil {
		t.Fatalf("env --env-file: %v", err)
	}
	if !strings.Contains(out, `"X": "42"`) {
		t.Fatalf("env --env-file did not surface the extra layer: %s", out)
	}
}

// TestEnvFile_MissingIsAnError: a typo must fail loudly, not silently skip.
func TestEnvFile_MissingIsAnError(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	dir := envFileFixture(t)
	_, err := runRoot(t, "--project-dir", dir, "gap-report", "--env-file", "nope.env")
	if err == nil {
		t.Fatal("missing --env-file must be an error")
	}
	if !strings.Contains(err.Error(), "nope.env") {
		t.Fatalf("error should name the file: %v", err)
	}
}
