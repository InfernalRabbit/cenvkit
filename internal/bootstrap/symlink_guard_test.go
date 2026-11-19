package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInit_RefusesSymlinkedExample guards CWE-59: a repository could commit an
// example.* symlink pointing at an arbitrary readable file (e.g. a credentials
// file), and a naive init would copy it into the repo where it can be committed.
func TestInit_RefusesSymlinkedExample(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "credentials")
	wf(t, secret, "AWS_SECRET_ACCESS_KEY=leak\n")

	dir := t.TempDir()
	if err := os.Symlink(secret, filepath.Join(dir, "example.env")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if err := Init(dir); err == nil {
		t.Fatal("Init must refuse to seed from a symlinked example.*")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		t.Fatal("Init copied a symlinked source into the repository")
	}
}

// TestInit_DoesNotWriteThroughDanglingSymlinkTarget guards the write side: a
// committed dangling .env symlink must not become a write outside the project.
func TestInit_DoesNotWriteThroughDanglingSymlinkTarget(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "target.env")

	dir := t.TempDir()
	wf(t, filepath.Join(dir, "example.env"), "X=1\n")
	if err := os.Symlink(target, filepath.Join(dir, ".env")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if err := Init(dir); err != nil {
		t.Fatalf("a dangling target counts as existing (no-clobber), so init must not fail: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("Init wrote OUTSIDE the project through a dangling symlink")
	}
}
