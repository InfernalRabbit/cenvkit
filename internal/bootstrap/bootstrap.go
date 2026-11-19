// Package bootstrap implements `cenvkit init`: seed .<X> from example.<X>
// no-clobber, fanning out one directory level. No sudo, no chmod 777, never
// overwrites an existing file (secret-wipe guard).
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Init seeds the given directory and its immediate subdirectories.
func Init(dir string) error {
	if err := seedDir(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue // symlinks: DirEntry.IsDir() is false, so they are never fanned out
		}
		if err := seedDir(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// seedDir copies each example.<X> to .<X> when the target does not already exist.
func seedDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "example") {
			continue
		}
		target := strings.TrimPrefix(e.Name(), "example") // example.env -> .env
		if target == "" || target == e.Name() {
			continue
		}
		// CWE-59: seed ONLY regular files. A committed example.* SYMLINK would
		// otherwise be followed as a source, copying an arbitrary readable file
		// (e.g. a credentials file) into the repository, where it can be committed.
		src := filepath.Join(dir, e.Name())
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return fmt.Errorf("stat %s: %w", src, err)
		}
		if !srcInfo.Mode().IsRegular() {
			return fmt.Errorf("%s: refusing to seed from a non-regular file (symlinks are not followed)", e.Name())
		}
		dst := filepath.Join(dir, target)
		// Lstat, not Stat: a DANGLING symlink still counts as existing, so we never
		// write through it (writing through would create a file OUTSIDE the project).
		if _, err := os.Lstat(dst); err == nil {
			continue // no-clobber
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", dst, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		// O_CREAT|O_EXCL refuses to create through a symlink that appeared after the
		// Lstat above (EEXIST on any existing entry, including a dangling link).
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create %s: %w", dst, err)
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil {
			return fmt.Errorf("write %s: %w", dst, werr)
		}
		if cerr != nil {
			return fmt.Errorf("write %s: %w", dst, cerr)
		}
	}
	return nil
}
