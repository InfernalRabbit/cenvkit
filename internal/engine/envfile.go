package engine

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/docker/cli/pkg/kvfile"
)

func init() {
	// Docker Compose registers this format itself; compose-go only includes the
	// standard dotenv format. Reuse the same Docker CLI parser for raw values.
	dotenv.RegisterFormat("raw", parseRawEnv)
}

func parseRawEnv(r io.Reader, filename string, values map[string]string, lookup func(string) (string, bool)) error {
	lines, err := kvfile.ParseFromReader(r, lookup)
	if err != nil {
		return fmt.Errorf("parse raw env_file %s: %w", filename, err)
	}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		values[key] = value
	}
	return nil
}

// parseEnvFile keeps per-file declarations for attribution while allowing the
// upstream parser to see external values, this file, and earlier files in order.
func parseEnvFile(path, format string, previous map[string]string, external dotenv.LookupFn) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	values := map[string]string{}
	err = dotenv.ParseWithFormat(f, path, values, func(k string) (string, bool) {
		if v, ok := external(k); ok {
			return v, true
		}
		if v, ok := values[k]; ok {
			return v, true
		}
		v, ok := previous[k]
		return v, ok
	}, format)
	return values, err
}
