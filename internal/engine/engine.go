// Package engine wraps compose-go (v2.17.0). It is the ONLY package in cenvkit
// that imports compose-go; everything else consumes the plain-Go Result/ProjectView.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"

	"github.com/InfernalRabbit/cenvkit/internal/provenance"
)

// Input describes one Layer-2 resolution request.
type Input struct {
	ProjectDir  string   // absolute working dir
	ConfigFiles []string // explicit -f; empty => COMPOSE_FILE / default discovery
	Env         []string // chain.Result.Vars — seeds interpolation
	Profiles    []string // active profiles (M3)
}

// ProjectView is a compose-go-free projection so internal/provenance and cmd
// never import compose-go.
type ProjectView struct {
	WorkingDir string
	Services   map[string][]string // service -> existing resolved env_file abs paths
}

// Result is the active runtime-only env_file set for debugging, never appended
// to the Layer-1 run chain.
type Result struct {
	EnvFiles []string // absolute, existing, active-only, deterministically ordered, deduped
	Project  ProjectView
}

// Engine is the seam. One real impl over compose-go; trivially fakeable in tests.
type Engine interface {
	Resolve(ctx context.Context, in Input) (Result, error)
	Provenance(ctx context.Context, in ProvInput) (provenance.Report, error)
}

type composeEngine struct{}

// ErrNoComposeFile distinguishes absent default configuration from load errors.
var ErrNoComposeFile = errors.New("no compose file found")

// New returns the compose-go-backed Engine, pinned to v2.17.0.
func New() Engine { return &composeEngine{} }

func (e *composeEngine) Resolve(ctx context.Context, in Input) (Result, error) {
	// COMPOSE_FILE selection + ${VAR} interpolation is the cenvkit-side seam:
	// probe-verified (compose-go v2.17.0, see docs/agent-notes/compose-go.md)
	// that cli.WithConfigFileEnv (a) only sees COMPOSE_FILE if WithEnv ran first,
	// and (b) os.Stats the RAW string with NO ${VAR} interpolation and resolves
	// relative entries against the PROCESS cwd, not WithWorkingDirectory. So when
	// in.ConfigFiles is empty we compute the config list ourselves (interpolated,
	// joined-to-abs against in.ProjectDir) and pass it as the configs arg.
	configs := in.ConfigFiles
	if len(configs) == 0 {
		var err error
		configs, err = resolveComposeFiles(in.ProjectDir, in.Env)
		if err != nil {
			return Result{}, fmt.Errorf("resolve compose files: %w", err)
		}
	}
	if len(configs) == 0 {
		return Result{}, ErrNoComposeFile
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(in.Env),
		cli.WithDefaultProfiles(in.Profiles...),
		cli.WithResolvedPaths(true),      // EnvFile.Path => absolute
		cli.WithInterpolation(true),      // ${...} in paths resolves from in.Env
		cli.WithoutEnvironmentResolution, // D1 lever: missing required env_file does not abort
	)
	if err != nil {
		return Result{}, fmt.Errorf("compose project options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load compose project: %w", err)
	}

	view := ProjectView{WorkingDir: proj.WorkingDir, Services: map[string][]string{}}
	var out []string
	seen := map[string]bool{}

	// Deterministic emission: sort service names; preserve env_file order within a service.
	names := make([]string, 0, len(proj.Services))
	for name := range proj.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := proj.Services[name]
		var existing []string
		for _, ef := range svc.EnvFiles {
			if _, statErr := os.Stat(ef.Path); statErr != nil {
				continue // D1 skip-missing parity (compose-go keeps the path; we drop it)
			}
			existing = append(existing, ef.Path)
			if !seen[ef.Path] {
				seen[ef.Path] = true
				out = append(out, ef.Path)
			}
		}
		view.Services[name] = existing
	}
	return Result{EnvFiles: out, Project: view}, nil
}
