package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookPath_MergedEnvironment(t *testing.T) {
	dir := t.TempDir()
	name := "cenvkit-lookup-child"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := lookPathInEnv(strings.TrimSuffix(name, ".exe"), map[string]string{"PATH": dir})
	if err != nil || found != path {
		t.Fatalf("merged PATH did not select child: path=%q err=%v", found, err)
	}
	_, err = lookPathInEnv(name, map[string]string{"PATH": t.TempDir()})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("empty search directory must not use process PATH: %v", err)
	}
}

func TestExpansionFlagFalse(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".env"), "CK_EXPAND=${CK_EXPAND_BASE:-expanded}\n")
	t.Setenv("CK_EXPAND", "")
	// Unset the key itself; an empty shell value still wins over the chain.
	if err := os.Unsetenv("CK_EXPAND"); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"env", "run"} {
		args := []string{command, "--project-dir", dir, "--expand=false"}
		if command == "run" {
			args = append(args, "--print", "--", "unused")
		}
		root := newRootCmd()
		var output strings.Builder
		root.SetOut(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `\${CK_EXPAND_BASE:-expanded}`) {
			t.Fatalf("%s --expand=false still expanded: %s", command, output.String())
		}
	}
}

func TestWrapperFlags_MissingValues(t *testing.T) {
	for _, args := range [][]string{{"config", "--color"}, {"--chain", "--project-dir", "/tmp"}} {
		_, err := extractWrapperFlags(newRootCmd(), args)
		var ee *exitError
		if !errors.As(err, &ee) || ee.code != 2 {
			t.Fatalf("missing wrapper flag value should exit 2: %v", err)
		}
	}
}
