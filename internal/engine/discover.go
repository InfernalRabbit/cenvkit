package engine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/template"
)

func seedLookup(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

// resolveComposeFiles preserves explicit overlays, including missing paths so
// loading reports errors. Default discovery delegates base/override precedence
// and parent-directory search to compose-go.
func resolveComposeFiles(dir string, env []string) ([]string, error) {
	cf := seedLookup(env, "COMPOSE_FILE")
	if cf == "" {
		opts, err := cli.NewProjectOptions(nil,
			cli.WithWorkingDirectory(dir), cli.WithDefaultConfigPath)
		if err != nil {
			return nil, err
		}
		return opts.ConfigPaths, nil
	}
	sep := seedLookup(env, "COMPOSE_PATH_SEPARATOR")
	if sep == "" {
		sep = string(os.PathListSeparator) // ":" on unix — NEVER ","
	}
	var out []string
	vars := envSliceToMap(env)
	for _, raw := range strings.Split(cf, sep) {
		entry := strings.ReplaceAll(raw, "${ENV}", "${CENVKIT_ENV}")
		f, err := template.SubstituteWithOptions(entry, func(k string) (string, bool) {
			v, ok := vars[k]
			return v, ok
		}, template.WithoutLogging)
		if err != nil {
			return nil, err
		}
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f) // resolve against ProjectDir, NOT process cwd
		}
		out = append(out, f)
	}
	return out, nil
}

// HasComposeFileEnv is a presence probe only. Loaders use resolveComposeFiles
// directly so a missing explicit config never silently disappears.
func HasComposeFileEnv(dir string, env []string) bool {
	configs, err := resolveComposeFiles(dir, env)
	if err != nil {
		return false
	}
	for _, path := range configs {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// HasComposeFile is the single-COMPOSE_FILE-value convenience form (still
// interpolates ${CENVKIT_ENV} when the value carries it AND the caller seeds
// CENVKIT_ENV — prefer HasComposeFileEnv from cmd code, which threads the full env).
func HasComposeFile(dir, composeFileEnv string) bool {
	if composeFileEnv != "" {
		return HasComposeFileEnv(dir, []string{"COMPOSE_FILE=" + composeFileEnv})
	}
	return HasComposeFileEnv(dir, nil)
}
