package engine

import (
	"path/filepath"
	"testing"
)

// TestResolveComposeFiles_CustomPathSeparator covers the documented contract:
// COMPOSE_FILE entries split on COMPOSE_PATH_SEPARATOR when set, else on the OS
// path-list separator, and NEVER on a comma.
func TestResolveComposeFiles_CustomPathSeparator(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.yml")
	touch(t, dir, "b.yml")

	got, err := resolveComposeFiles(dir, []string{
		"COMPOSE_FILE=a.yml;b.yml",
		"COMPOSE_PATH_SEPARATOR=;",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("custom separator: want 2 paths, got %d: %v", len(got), got)
	}
	if got[0] != filepath.Join(dir, "a.yml") || got[1] != filepath.Join(dir, "b.yml") {
		t.Fatalf("custom separator: unexpected paths %v", got)
	}

	// Without the override, ';' is not a separator: the whole value is ONE path.
	got, err = resolveComposeFiles(dir, []string{"COMPOSE_FILE=a.yml;b.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("default separator: want 1 path, got %d: %v", len(got), got)
	}
}
