package main

import (
	"strings"
	"testing"
)

// TestHelp_ReflectsCurrentState guards the help text against drift: the commands
// and flags that define the current CLI must be discoverable from --help.
func TestHelp_ReflectsCurrentState(t *testing.T) {
	t.Setenv("CENVKIT_ENV", "")
	rootHelp, err := runRoot(t, "--help")
	if err != nil {
		t.Fatalf("root help: %v", err)
	}
	for _, want := range []string{"env-debug", "gap-report", "cenvkit compose up -d", "cenvkit env-debug --var"} {
		if !strings.Contains(rootHelp, want) {
			t.Fatalf("root help lost %q:\n%s", want, rootHelp)
		}
	}

	debugHelp, err := runRoot(t, "env-debug", "--help")
	if err != nil {
		t.Fatalf("env-debug help: %v", err)
	}
	for _, want := range []string{"--show", "deps", "--redact", "cascading", "Exit codes", "--var alone implies trace"} {
		if !strings.Contains(debugHelp, want) {
			t.Fatalf("env-debug help lost %q:\n%s", want, debugHelp)
		}
	}
}
