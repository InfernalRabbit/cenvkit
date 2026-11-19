package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func environmentValue(env map[string]string, key string) string {
	if value, ok := env[key]; ok {
		return value
	}
	if runtime.GOOS == "windows" {
		for name, value := range env {
			if strings.EqualFold(name, key) {
				return value
			}
		}
	}
	return ""
}

// lookPathInEnv uses native executable checks for explicit candidates without
// temporarily mutating the process environment (which would race other users).
func lookPathInEnv(file string, env map[string]string) (string, error) {
	path := environmentValue(env, "PATH")
	ext := environmentValue(env, "PATHEXT")
	if path == os.Getenv("PATH") && (runtime.GOOS != "windows" || ext == os.Getenv("PATHEXT")) {
		return exec.LookPath(file)
	}
	separators := "/"
	if runtime.GOOS == "windows" {
		separators = `:\/`
	}
	if strings.ContainsAny(file, separators) {
		return findExecutableInEnv(file, ext)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			if runtime.GOOS == "windows" {
				continue
			}
			dir = "."
		}
		candidate := filepath.Join(dir, file)
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		resolved, err := findExecutableInEnv(absolute, ext)
		if err == nil {
			if !filepath.IsAbs(candidate) {
				return "", &exec.Error{Name: file, Err: exec.ErrDot}
			}
			return resolved, nil
		}
	}
	return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
}

func findExecutableInEnv(path, pathExt string) (string, error) {
	if runtime.GOOS != "windows" {
		return exec.LookPath(path)
	}
	// Windows' executable check is a stat, not Unix execute permission bits.
	// Do not call LookPath here: it would append the wrapper's PATHEXT again.
	check := func(candidate string) (string, error) {
		info, err := os.Stat(candidate)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", os.ErrPermission
		}
		return candidate, nil
	}
	if filepath.Ext(path) != "" {
		if found, err := check(path); err == nil {
			return found, nil
		}
	}
	if pathExt == "" {
		pathExt = ".com;.exe;.bat;.cmd"
	}
	for _, ext := range strings.Split(pathExt, ";") {
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if found, err := check(path + ext); err == nil {
			return found, nil
		}
	}
	return "", &exec.Error{Name: path, Err: exec.ErrNotFound}
}
