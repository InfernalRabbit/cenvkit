// Command cenvkit assembles COMPOSE_ENV_FILES from a layered env chain and
// execs `docker compose`. See docs/cenvkit.md for the behavior reference.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/InfernalRabbit/cenvkit/internal/bootstrap"
	"github.com/InfernalRabbit/cenvkit/internal/chain"
	"github.com/InfernalRabbit/cenvkit/internal/engine"
	"github.com/InfernalRabbit/cenvkit/internal/envfiles"
	"github.com/InfernalRabbit/cenvkit/internal/envmap"
	"github.com/InfernalRabbit/cenvkit/internal/provenance"
	"github.com/InfernalRabbit/cenvkit/internal/style"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

// composeGoVersion returns the linked compose-go module version read at runtime
// from the binary's build info. Returns "" if build info is unavailable (e.g.
// in some test harnesses) or the dep is not found.
func composeGoVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/compose-spec/compose-go/v2" {
			return dep.Version
		}
	}
	return ""
}

// resolveVersion returns a meaningful version string for any build path:
//   - ldflags-injected (release or `make install` git-describe stamp): returned as-is.
//   - `go install module@vX` / `@latest`: bi.Main.Version is a real or pseudo-version.
//   - plain `go build`: vcs.revision from build settings gives dev+<commit>[-dirty].
//   - no build info: falls back to the bare "dev" default.
func resolveVersion() string {
	if version != "dev" {
		return version // release build or Makefile git-describe stamp; do not override
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version // go install module@vX or @latest
	}
	// Plain `go build`: derive from VCS info embedded by the toolchain.
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	v := "dev+" + rev
	if modified == "true" {
		v += "-dirty"
	}
	return v
}

// styler is resolved once in the root PersistentPreRunE from --color and used by
// every subcommand for human output. currentStyler() is nil-safe so a subcommand
// invoked without the root pre-run (e.g. a direct unit test) still gets plain.
var styler provenance.Styler

func currentStyler() provenance.Styler {
	if styler == nil {
		return style.Disabled()
	}
	return styler
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cenvkit",
		Short:         "Layered env-file assembly for Docker Compose",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Resolve ONE styler for the whole invocation from --color (+ NO_COLOR /
		// CLICOLOR_FORCE / TTY, handled by termenv). Stored in a package var so every
		// subcommand uses the same decision; gated on os.Stdout (human output). The
		// --json path swaps in style.Disabled() at its own call site (top precedence).
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			flag := "auto"
			if f := cmd.Root().PersistentFlags().Lookup("color"); f != nil {
				flag = f.Value.String()
			}
			styler = style.Resolve(flag, os.Stdout)
			// COMPOSE_ENV was removed (renamed to CENVKIT_ENV). It is still common in
			// old wrappers, and silently ignoring it would select the wrong tier.
			if v := os.Getenv("COMPOSE_ENV"); v != "" {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"cenvkit: warning: COMPOSE_ENV=%s is ignored (removed name); use CENVKIT_ENV\n", v)
			}
			return nil
		},
	}
	root.PersistentFlags().String("project-dir", "", "project directory (default: current directory)")
	root.PersistentFlags().String("color", "auto", "colorize output: auto|always|never")
	// --chain selects a named [name] section from .cenvkit.envchain (C4). It is
	// persistent (mirrors --project-dir) so every chain-reading subcommand inherits
	// ONE definition via resolveChainName; "" / "default" = the implicit
	// header-less/[default] section. Commands that never read the chain (version,
	// init) inherit it harmlessly, exactly as they inherit --project-dir.
	root.PersistentFlags().String("chain", "", "named chain section from .cenvkit.envchain (default: the header-less/[default] chain)")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the cenvkit version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, resolveVersion())
			if cgv := composeGoVersion(); cgv != "" {
				_, _ = fmt.Fprintln(out, "compose-go "+cgv)
			}
			return nil
		},
	})
	root.AddCommand(newEnvFilesCmd(), newComposeCmd(), newValidateCmd(),
		newInitCmd(), newEnvDebugCmd(), newGapReportCmd(),
		newRunCmd(), newEnvCmd())
	// COMPOSE_DEPTH is accepted-but-ignored (spec §5): include-graph load makes it obsolete.
	return root
}

// exitError carries a process exit code out of a command's RunE so main() can
// os.Exit with it. An empty msg means the command already wrote its own output
// (e.g. the gap report) and only the code should propagate.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
func (e *exitError) ExitCode() int { return e.code }

// resolveProjectDir honors --project-dir, defaulting to the current directory.
// --project-dir is a PERSISTENT flag on the root. cmd.Flags() does NOT surface it
// when cmd is the root itself (cobra only merges inherited flags into a
// *subcommand*'s Flags() at parse/Execute time), so reading it that way returns ""
// when resolveProjectDir is called standalone. cmd.Flag() searches the local,
// inherited, and persistent flag sets WITHOUT requiring a merge, so it works for
// every receiver — the root, any subcommand, and the compose-path override
// (newComposeCmd's cmd.Flags().Set writes through to this same shared flag).
func resolveProjectDir(cmd *cobra.Command) (string, error) {
	pd := ""
	if f := cmd.Flag("project-dir"); f != nil {
		pd = f.Value.String()
	}
	if pd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		pd = wd
	}
	return filepath.Abs(pd)
}

// resolveChainName reads the persistent --chain flag (C4 named-chain selector),
// mirroring resolveProjectDir. cmd.Flag() searches local/inherited/persistent
// sets without a merge, so it works for the root, any subcommand, and the compose
// pre-scan override (which writes through to this same shared flag). "" means the
// implicit header-less/[default] chain — chain.Resolve normalizes it.
func resolveChainName(cmd *cobra.Command) string {
	if f := cmd.Flag("chain"); f != nil {
		return f.Value.String()
	}
	return ""
}

// assemble resolves the Layer-1 chain and returns the COMPOSE_ENV_FILES list and
// the chain result. v3 (Layer-2 debug-only): the run path is Layer-1 ONLY — a
// service `env_file:` is runtime-only (native per-container) and is deliberately
// NOT folded into COMPOSE_ENV_FILES. So we pass nil as the Layer-2 arg and skip
// engine.Resolve entirely (D1: faster compose/env-files; the engine's Layer-2
// enumeration now lives only in env-debug as a gap-detector).
//
// envOverlay entries (e.g. "CENVKIT_ENV=prod") are appended AFTER os.Environ() so
// they win via chain's osEnvMap last-wins — this is how `validate --all` re-resolves
// the Layer-1 chain per env (findings [3]/[9]).
func assemble(cmd *cobra.Command, envOverlay ...string) ([]string, chain.Result, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return nil, chain.Result{}, err
	}
	osEnv := append(os.Environ(), envOverlay...)
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname, Chain: resolveChainName(cmd)})
	if err != nil {
		return nil, chain.Result{}, err
	}
	merged, err := envfiles.Assemble(cr.Files, nil)
	if err != nil {
		return nil, chain.Result{}, err
	}
	return merged, cr, nil
}

// resolvePopulator is the shared front half of `run` and `env`: resolve the
// project dir, build the process env (os.Environ plus an optional `-e ENV`
// overlay), resolve the Layer-1 chain for THAT env, and flatten it into a merged
// shell-wins environment via internal/envmap.
//
// The `-e` overlay is a "CENVKIT_ENV=<v>" entry appended AFTER os.Environ() so it
// wins via chain's osEnvMap last-wins (the same mechanism `validate --all` uses,
// see newValidateCmd). It feeds BOTH the chain selection (which `.${ENV}.env` is
// picked) AND the flatten base, so the two stay consistent. expand toggles
// ${VAR} expansion (--expand default; --no-expand literal).
func resolvePopulator(cmd *cobra.Command, env string, expand bool, extraFiles []string) (envmap.Resolved, error) {
	dir, err := resolveProjectDir(cmd)
	if err != nil {
		return envmap.Resolved{}, err
	}
	osEnv := os.Environ()
	if env != "" {
		osEnv = append(osEnv, "CENVKIT_ENV="+env) // re-resolve the chain for this env; wins (last)
	}
	cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: osEnv, Hostname: os.Hostname, Chain: resolveChainName(cmd)})
	if err != nil {
		return envmap.Resolved{}, err
	}
	// envmap is fed ONLY the existence-filtered chain list (chain.Resolve drops
	// missing paths), so engine.Flatten's error-on-missing never fires for a
	// legitimately-absent chain slot. processEnv = the same osEnv used for
	// selection, so the flatten base and the shell-wins overlay match it; the
	// resolved selector is appended last so an env reference in a chain value
	// expands to the effective tier instead of an empty string.
	osEnv = withComposeEnv(osEnv, cr.ComposeEnv)
	files, err := addExtraEnvFiles(dir, cr.Files, extraFiles)
	if err != nil {
		return envmap.Resolved{}, err
	}
	return envmap.Resolve(envStringSliceToMap(osEnv), files, expand)
}

// addExtraEnvFiles resolves each --env-file path and appends it AFTER the project
// chain, so the file's values take part in interpolation exactly like
// `docker compose --env-file` (they still lose to the shell env, which is overlaid
// last). A missing file is an error, not a silent skip: docker compose errors too,
// and silently ignoring the flag is the failure mode this feature exists to fix.
func addExtraEnvFiles(projectDir string, files, extras []string) ([]string, error) {
	for _, p := range extras {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(projectDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("--env-file %s: %w", abs, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("--env-file %s: is a directory", abs)
		}
		files = append(files, abs)
	}
	return files, nil
}

// envStringSliceToMap turns os.Environ()-style "K=V" entries into a map. Later
// entries win (so an appended "-e" overlay overrides an inherited value).
func envStringSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// withComposeEnv returns env with the RESOLVED CENVKIT_ENV appended last.
//
// The selector resolves as shell CENVKIT_ENV > root .env > "dev" (chain.Resolve),
// but the "dev" default exists only in cenvkit's resolution — compose-go and the
// debug engine see the process environment, where a CENVKIT_ENV reference would
// expand to an empty string and, e.g., turn "compose.<env>.yml" into
// "compose..yml". Appending the resolved value last makes the child and the
// in-process engine agree with the chain on the effective selector. Never pass
// chain.Result.Vars here: engine.ProvInput.Env takes process env, not the seed.
func withComposeEnv(env []string, composeEnv string) []string {
	if composeEnv == "" {
		return env
	}
	return append(env, "CENVKIT_ENV="+composeEnv)
}

// composeEnvironment prevents native .env fallback when the selected chain is
// empty. Explicit Docker --env-file options still override its env-file default.
func composeEnvironment(files []string, composeEnv string, overlay ...string) []string {
	env := append(os.Environ(), "COMPOSE_ENV_FILES="+envfiles.Join(files))
	if len(files) == 0 {
		env = append(env, "COMPOSE_DISABLE_ENV_FILE=1")
	}
	return append(withComposeEnv(env, composeEnv), overlay...)
}

func newEnvFilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env-files",
		Short: "Print the resolved COMPOSE_ENV_FILES chain, one path per line",
		RunE: func(cmd *cobra.Command, _ []string) error {
			merged, _, err := assemble(cmd)
			if err != nil {
				return err
			}
			s := currentStyler()
			for _, f := range merged {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), s.Path(f)) // cyan on a TTY; plain when piped
			}
			return nil
		},
	}
}

func newComposeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "compose [args...]",
		Short:              "Assemble the chain and exec `docker compose`",
		DisableFlagParsing: true, // pass everything through to docker compose
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := extractWrapperFlags(cmd, args)
			if err != nil {
				return err
			}
			styler = style.Resolve(cmd.Flag("color").Value.String(), os.Stdout)
			merged, cr, err := assemble(cmd)
			if err != nil {
				return err
			}
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			dc := exec.Command("docker", append([]string{"compose"}, args...)...)
			dc.Dir = dir // the project dir the chain resolved in
			dc.Env = composeEnvironment(merged, cr.ComposeEnv)
			dc.Stdin, dc.Stdout, dc.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := startAndWait(dc); err != nil {
				if ee := asExitError(err); ee != nil {
					return childExitError(ee)
				}
				return fmt.Errorf("exec docker compose: %w", err)
			}
			return nil
		},
	}
	return c
}

// extractWrapperFlags parses persistent wrapper flags while leaving Docker's
// options untouched. A -- terminator protects flags belonging to child commands.
func extractWrapperFlags(cmd *cobra.Command, args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	flags := cmd.Root().PersistentFlags()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(out, args[i:]...), nil
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !strings.HasPrefix(arg, "--") || flags.Lookup(name) == nil {
			out = append(out, arg)
			continue
		}
		if !hasValue {
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, &exitError{code: 2, msg: "--" + name + " requires a value"}
			}
			i++
			value = args[i]
		}
		if err := flags.Set(name, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func newValidateCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate the resolved compose config (docker compose config -q)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := currentStyler()
			run := func(env string) error {
				var ov []string
				if env != "" {
					ov = []string{"CENVKIT_ENV=" + env} // re-resolve the Layer-1 chain for THIS env
				}
				merged, cr, err := assemble(cmd, ov...)
				if err != nil {
					return err
				}
				dir, err := resolveProjectDir(cmd)
				if err != nil {
					return err
				}
				dc := exec.Command("docker", "compose", "config", "-q")
				dc.Dir = dir
				dc.Env = composeEnvironment(merged, cr.ComposeEnv, ov...)
				dc.Stdout, dc.Stderr = os.Stdout, os.Stderr
				if err := startAndWait(dc); err != nil {
					if ee := asExitError(err); ee != nil {
						// Propagate docker's status (e.g. exit 1 for an invalid config)
						// instead of collapsing every failure to cenvkit's generic 1.
						return &exitError{code: childExitCode(ee), msg: "config invalid: docker compose config failed"}
					}
					return err
				}
				label := "config valid"
				if env != "" {
					label = env + " config valid"
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), s.Ok(label))
				return nil
			}
			if all {
				// Annotate BOTH tiers the same way so the messages stay consistent
				// (dev used to carry context while prod was raw).
				if err := run("dev"); err != nil {
					return annotateErr(err, "dev ")
				}
				if err := run("prod"); err != nil {
					return annotateErr(err, "prod ")
				}
				return nil
			}
			return run("")
		},
	}
	c.Flags().BoolVar(&all, "all", false, "validate both dev and prod")
	return c
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Seed .X from example.X (no-clobber) and fan out one level",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			return bootstrap.Init(dir)
		},
	}
}

// debugViewOrder is the canonical cascade order offered by --show.
var debugViewOrder = []provenance.View{
	provenance.ViewList,
	provenance.ViewFiles,
	provenance.ViewOverview,
	provenance.ViewEffective,
	provenance.ViewTrace,
}

// parseDebugView maps a --show value onto a View.
func parseDebugView(name string) (provenance.View, bool) {
	for _, v := range debugViewOrder {
		if string(v) == name {
			return v, true
		}
	}
	return "", false
}

func hasView(views []provenance.View, want provenance.View) bool {
	for _, v := range views {
		if v == want {
			return true
		}
	}
	return false
}

// perServiceView reports whether a view renders per-service data, i.e. whether
// --service can narrow it.
func perServiceView(v provenance.View) bool {
	return v == provenance.ViewFiles || v == provenance.ViewOverview ||
		v == provenance.ViewEffective || v == provenance.ViewTrace
}

// debugRequest is the validated shape of one env-debug invocation.
type debugRequest struct {
	views      []provenance.View
	varName    string
	service    string
	json       bool
	redact     bool
	value      bool
	wantLayers bool
}

// buildDebugRequest merges --show with the deprecated single-view flags, applies
// the documented defaults, and rejects impossible combinations with exit 2 (a
// usage error) instead of silently falling into one of the views.
func buildDebugRequest(shows []string, varName, service string, jsonOut, redact bool,
	aliasList, aliasFiles, aliasOverview, aliasEffective, aliasTrace, aliasValue bool) (debugRequest, error) {
	req := debugRequest{varName: varName, service: service, json: jsonOut, redact: redact, value: aliasValue}
	seen := map[provenance.View]bool{}
	add := func(v provenance.View) {
		if !seen[v] {
			seen[v] = true
			req.views = append(req.views, v)
		}
	}
	for _, name := range shows {
		v, ok := parseDebugView(name)
		if !ok {
			return req, &exitError{code: 2, msg: fmt.Sprintf("unknown --show %q (want list|files|overview|effective|trace)", name)}
		}
		add(v)
	}
	explicit := len(req.views) > 0
	for _, pair := range []struct {
		set bool
		v   provenance.View
	}{
		{aliasList, provenance.ViewList},
		{aliasFiles, provenance.ViewFiles},
		{aliasOverview, provenance.ViewOverview},
		{aliasEffective, provenance.ViewEffective},
		{aliasTrace, provenance.ViewTrace},
	} {
		if pair.set {
			explicit = true
			add(pair.v)
		}
	}
	if req.value && jsonOut {
		return req, &exitError{code: 2, msg: "--value cannot be combined with --json (use --json --var KEY)"}
	}
	if req.value {
		if varName == "" {
			return req, &exitError{code: 2, msg: "--value requires --var"}
		}
		if len(req.views) > 0 {
			return req, &exitError{code: 2, msg: "--value prints a bare value and cannot be combined with other views"}
		}
		return req, nil
	}
	if hasView(req.views, provenance.ViewTrace) && varName == "" {
		return req, &exitError{code: 2, msg: "--show trace requires --var"}
	}
	// Defaults: only for the human path. JSON without --show keeps the historical
	// whole-report document (backwards compatible), narrowed by the filters.
	if !jsonOut && len(req.views) == 0 {
		switch {
		case varName != "":
			add(provenance.ViewTrace)
		case service != "":
			add(provenance.ViewEffective)
		default:
			add(provenance.ViewList)
		}
	}
	if service != "" {
		per := false
		for _, v := range req.views {
			if perServiceView(v) {
				per = true
				break
			}
		}
		if !per && (explicit || !jsonOut) {
			return req, &exitError{code: 2, msg: fmt.Sprintf("--service %q needs a per-service view (--show files|overview|effective|trace)", service)}
		}
	}
	// Raw layers are only needed by --overview and by the --var filter over file
	// lists (human or JSON).
	needsLayersForFiles := varName != "" &&
		(hasView(req.views, provenance.ViewList) || hasView(req.views, provenance.ViewFiles) || jsonOut)
	req.wantLayers = hasView(req.views, provenance.ViewOverview) || needsLayersForFiles
	return req, nil
}

func newEnvDebugCmd() *cobra.Command {
	var (
		shows    []string
		varName  string
		service  string
		jsonOut  bool
		redact   bool
		envFiles []string
		// Deprecated single-view flags: aliases for --show, scheduled for removal
		// in 0.9. Each still works and appends its view in canonical order.
		aliasList, aliasFiles, aliasOverview, aliasEffective, aliasTrace, aliasValue bool
	)
	c := &cobra.Command{
		Use:   "env-debug",
		Short: "Inspect the resolved env chain with provenance",
		Long: "Inspect the resolved env chain with provenance.\n\n" +
			"Views CASCADE: repeat --show and the sections render in the order given\n" +
			"(no --show => the Layer-1 list). Filters narrow every view with the data:\n" +
			"--var KEY on list/files/overview/effective/trace/json, --service NAME on the\n" +
			"per-service views. Impossible combinations exit 2 instead of guessing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := buildDebugRequest(shows, varName, service, jsonOut, redact,
				aliasList, aliasFiles, aliasOverview, aliasEffective, aliasTrace, aliasValue)
			if err != nil {
				return err
			}
			if !req.json {
				for _, d := range []struct {
					used bool
					name string
				}{
					{aliasList, "list"},
					{aliasFiles, "files"},
					{aliasOverview, "overview"},
					{aliasEffective, "effective"},
					{aliasTrace, "trace"},
				} {
					if d.used {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cenvkit: warning: --%s is deprecated; use --show %s\n", d.name, d.name)
					}
				}
			}
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname, Chain: resolveChainName(cmd)})
			if err != nil {
				return err
			}
			chainFiles, err := addExtraEnvFiles(dir, cr.Files, envFiles)
			if err != nil {
				return err
			}
			pf := make([]engine.ProvFile, 0, len(chainFiles))
			for _, f := range chainFiles {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: withComposeEnv(os.Environ(), cr.ComposeEnv), EnvFiles: pf,
				WantLayers: req.wantLayers,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if req.json {
				// JSON is never colored and the filter/section options are honoured
				// rather than silently dropped.
				return provenance.RenderJSONOpts(out, rep, provenance.JSONOpts{
					Service: req.service, Var: req.varName, Views: req.views, Redact: req.redact,
					DropLayers: !hasView(req.views, provenance.ViewOverview),
				})
			}
			opts := provenance.HumanOpts{
				Views: req.views, Service: req.service, FilterVar: req.varName,
				Redact:           req.redact,
				ComposeEnv:       cr.ComposeEnv,
				ComposeEnvSource: cr.ComposeEnvSource,
				ProjectDir:       dir,
				Style:            currentStyler(),
			}
			if req.value {
				opts.Value = req.varName
			}
			if hasView(req.views, provenance.ViewTrace) {
				opts.Trace = req.varName
			}
			provenance.RenderHuman(out, rep, opts)
			return nil
		},
	}
	c.Flags().StringArrayVar(&shows, "show", nil, "view to render, repeatable and cascading: list|files|overview|effective|trace")
	c.Flags().StringVar(&varName, "var", "", "filter every view by variable KEY (implies --show trace when no view is given)")
	c.Flags().StringVar(&service, "service", "", "filter per-service views by SERVICE (implies --show effective when no view is given)")
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON; --show selects the sections, filters apply")
	c.Flags().BoolVar(&redact, "redact", false, "mask values as <redacted>, keeping keys, files and layers")
	c.Flags().BoolVar(&aliasValue, "value", false, "print the winning value of --var (non-cascading primitive)")
	c.Flags().BoolVar(&aliasList, "list", false, "Layer-1 chain files (deprecated: --show list)")
	c.Flags().BoolVar(&aliasFiles, "files", false, "interpolation + runtime-only split (deprecated: --show files)")
	c.Flags().BoolVar(&aliasOverview, "overview", false, "per-file layering (deprecated: --show overview)")
	c.Flags().BoolVar(&aliasEffective, "effective", false, "per-service env (deprecated: --show effective)")
	c.Flags().BoolVar(&aliasTrace, "trace", false, "trace --var (deprecated: --show trace)")
	c.Flags().StringArrayVar(&envFiles, "env-file", nil,
		"extra env file layered after the chain (repeatable; mirrors docker compose --env-file)")
	// Hidden, not cobra-deprecated: cobra prints its deprecation note to stderr on
	// every use, which corrupts machine-readable output for callers that capture
	// both streams. The warning below is emitted only on the human path.
	for _, name := range []string{"list", "files", "overview", "effective", "trace"} {
		_ = c.Flags().MarkHidden(name)
	}
	return c
}

// newGapReportCmd is the daemon-free CI/pre-build lint over the EXISTING gap set
// (engine.Provenance's VarTrace.Gap, internal/engine/provenance.go:424). It loads
// the compose model in process — never execs docker — and maps the result to a
// distinct exit-code contract via exitError: gaps found => 1, clean => 0, no
// compose file discovered => 2 (a misconfiguration, NOT a clean pass; spec §6).
func newGapReportCmd() *cobra.Command {
	var (
		jsonOut  bool
		envFiles []string
	)
	c := &cobra.Command{
		Use:   "gap-report",
		Short: "Report env_file:->${VAR} interpolation gaps (CI/pre-build lint; daemon-free)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := resolveProjectDir(cmd)
			if err != nil {
				return err
			}
			cr, err := chain.Resolve(chain.Input{ProjectDir: dir, OSEnv: os.Environ(), Hostname: os.Hostname, Chain: resolveChainName(cmd)})
			if err != nil {
				return err
			}
			chainFiles, err := addExtraEnvFiles(dir, cr.Files, envFiles)
			if err != nil {
				return err
			}
			pf := make([]engine.ProvFile, 0, len(chainFiles))
			for _, f := range chainFiles {
				pf = append(pf, engine.ProvFile{Path: f, Layer: "layer1"})
			}
			rep, err := engine.New().Provenance(cmd.Context(), engine.ProvInput{
				ProjectDir: dir, Env: withComposeEnv(os.Environ(), cr.ComposeEnv), EnvFiles: pf, RequireCompose: true,
			})
			if errors.Is(err, engine.ErrNoComposeFile) {
				return &exitError{code: 2, msg: "no compose file found in " + dir}
			}
			if err != nil {
				return err
			}
			gr := provenance.CollectGaps(rep)
			out := cmd.OutOrStdout()
			if jsonOut {
				if err := provenance.RenderGapReportJSON(out, gr); err != nil {
					return err
				}
			} else {
				provenance.RenderGapReportHuman(out, gr, currentStyler())
			}
			if gr.Count > 0 {
				return &exitError{code: 1} // gaps found; report already printed (no extra msg)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&jsonOut, "json", false, "machine-readable JSON")
	c.Flags().StringArrayVar(&envFiles, "env-file", nil,
		"extra env file layered after the chain (repeatable; mirrors docker compose --env-file)")
	return c
}

// newEnvCmd emits the chain-derived environment (the no-docker "local arm").
// It prints ONLY the keys the chain contributed, key-sorted, with the shell
// overlaid (shell-wins) — it never dumps the whole inherited process env. An
// empty/all-missing chain prints nothing and exits 0 (a fresh repo is legitimate).
func newEnvCmd() *cobra.Command {
	var (
		envSel   string
		expand   bool
		noExpand bool
		format   string
		envFiles []string
	)
	c := &cobra.Command{
		Use:   "env",
		Short: "Print the chain-derived environment (dotenv|json|shell)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := envmap.ParseFormat(format)
			if err != nil {
				return err
			}
			res, err := resolvePopulator(cmd, envSel, expand && !noExpand, envFiles)
			if err != nil {
				return err
			}
			return envmap.Emit(cmd.OutOrStdout(), res, f)
		},
	}
	c.Flags().StringVarP(&envSel, "env", "e", "", "select the chain env (overrides CENVKIT_ENV); e.g. -e prod")
	c.Flags().BoolVar(&expand, "expand", true, "expand ${VAR} in chain values (default)")
	c.Flags().BoolVar(&noExpand, "no-expand", false, "emit chain values literally, leaving ${VAR} unexpanded")
	c.MarkFlagsMutuallyExclusive("expand", "no-expand")
	c.Flags().StringVar(&format, "format", "dotenv", "output format: dotenv|json|shell")
	c.Flags().StringArrayVar(&envFiles, "env-file", nil,
		"extra env file layered after the chain (repeatable; mirrors docker compose --env-file)")
	return c
}

// newRunCmd flattens the chain and execs a command with the merged environment
// (shell-wins). `--` is REQUIRED so cenvkit's own flags never collide with the
// child's; everything after `--` is the command, passed verbatim. `--print` dumps
// the chain-derived env (dotenv) and exits 0 WITHOUT exec'ing.
func newRunCmd() *cobra.Command {
	var (
		envSel   string
		expand   bool
		noExpand bool
		print    bool
		envFiles []string
	)
	c := &cobra.Command{
		Use:                   "run [-e ENV] [--expand|--no-expand] [--print] -- <cmd> [args...]",
		Short:                 "Exec a command with the chain-derived environment",
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// `--` enforcement: ArgsLenAtDash() is the count of args BEFORE `--` when
			// present, and -1 when absent (cobra v1.10.2, probe-verified). args already
			// has everything before `--` stripped and flag-parsing stops at `--`, so a
			// child flag like `run -- echo --foo` passes `--foo` through untouched.
			if cmd.ArgsLenAtDash() < 0 {
				return &exitError{code: 2, msg: "run requires `--` before the command (e.g. cenvkit run -- npm test)"}
			}
			if len(args) == 0 {
				return &exitError{code: 2, msg: "no command after `--`"}
			}
			res, err := resolvePopulator(cmd, envSel, expand && !noExpand, envFiles)
			if err != nil {
				return err
			}
			if print {
				// --print: dump the chain-derived env (identical to `env --format
				// dotenv`), skip exec, exit 0. Reveals plaintext secrets by design.
				return envmap.EmitDotenv(cmd.OutOrStdout(), res)
			}
			return execChild(args, res.Full)
		},
	}
	c.Flags().StringVarP(&envSel, "env", "e", "", "select the chain env (overrides CENVKIT_ENV); e.g. -e prod")
	c.Flags().BoolVar(&expand, "expand", true, "expand ${VAR} in chain values (default)")
	c.Flags().BoolVar(&noExpand, "no-expand", false, "emit chain values literally, leaving ${VAR} unexpanded")
	c.MarkFlagsMutuallyExclusive("expand", "no-expand")
	c.Flags().BoolVar(&print, "print", false, "print the chain-derived env (dotenv) and exit; do not exec")
	c.Flags().StringArrayVar(&envFiles, "env-file", nil,
		"extra env file layered after the chain (repeatable; mirrors docker compose --env-file)")
	return c
}

// execChild runs args[0] with args[1:] and the given merged environment,
// forwarding stdio and signals, and maps the outcome to a POSIX-parity exit code
// via exitError (MF6):
//   - clean exit            → the child's own code (0 = nil error)
//   - child exits non-zero  → *exec.ExitError → its ExitCode()
//   - child killed by signal → 128 + signo
//   - command not found      → 127
//   - found but not executable → 126
//
// We use exec.Command (not syscall.Exec) so --print can run earlier without an
// ordering trap and so signal forwarding + exit mapping are explicit and testable.
func execChild(args []string, env map[string]string) error {
	bin := args[0]
	// Command's default lookup reads the wrapper's PATH, not Cmd.Env. Resolve
	// against the merged child environment before constructing the command.
	path, err := lookPathInEnv(bin, env)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return &exitError{code: 127, msg: bin + ": command not found"}
		}
		if errors.Is(err, os.ErrPermission) {
			return &exitError{code: 126, msg: bin + ": permission denied"}
		}
		return &exitError{code: 127, msg: bin + ": " + err.Error()}
	}

	c := &exec.Cmd{Path: path, Args: args}
	c.Env = mapToEnvSlice(env)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := startAndWait(c); err != nil {
		if ee := asExitError(err); ee != nil {
			return childExitError(ee) // child exited non-zero / was signalled
		}
		// Start failed after LookPath succeeded (rare: file changed under us).
		if errors.Is(err, os.ErrPermission) {
			return &exitError{code: 126, msg: bin + ": permission denied"}
		}
		return &exitError{code: 127, msg: bin + ": " + err.Error()}
	}
	return nil // child exited 0
}

// startAndWait starts c and waits for it, relaying the terminating signals to the
// child meanwhile. A bare Run would leave the child (e.g. `docker compose`, which
// pulls containers) orphaned when cenvkit itself is signalled by a supervisor or a
// CI timeout.
func startAndWait(c *exec.Cmd) error {
	if err := c.Start(); err != nil {
		return err
	}
	// Scoped to the real terminating set (NOT a bare Notify) so we don't relay
	// runtime-internal signals like SIGURG/SIGCHLD that Go uses.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-sigCh:
				_ = c.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	return c.Wait()
}

// asExitError returns err as an *exec.ExitError, or nil when it is not one.
func asExitError(err error) *exec.ExitError {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee
	}
	return nil
}

// childExitCode maps a child's wait status to POSIX parity: a signal-terminated
// child is 128+signo, an ordinary exit keeps its own code, and ExitCode() == -1
// (killed, no wait status) is reported as 1 rather than leaking 255.
func childExitCode(ee *exec.ExitError) int {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if code := ee.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

// childExitError wraps a child's non-zero status as an exitError. The message is
// empty on purpose: the child already wrote its own diagnostics to stderr.
func childExitError(ee *exec.ExitError) error { return &exitError{code: childExitCode(ee)} }

// annotateErr prefixes a failure with a tier label while PRESERVING its exit code.
// A plain fmt.Errorf would keep the code discoverable for errors.As but main prints
// the inner exitError's message only, silently dropping the wrapper's context.
func annotateErr(err error, prefix string) error {
	var ee *exitError
	if errors.As(err, &ee) {
		return &exitError{code: ee.code, msg: prefix + ee.msg}
	}
	return fmt.Errorf("%s%w", prefix, err)
}

// mapToEnvSlice turns a merged env map into a sorted os.Environ()-style slice for
// exec. Sorting is purely for determinism (stable test goldens / reproducible
// process inspection); the OS does not care about order.
func mapToEnvSlice(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Errors go to stderr in red (spec §4). Gate on STDERR's TTY (not stdout,
		// which the PersistentPreRun styler used) and honor --color via a best-effort
		// args scan — the pre-run may not have run if parsing itself failed.
		es := style.Resolve(colorFlagFromArgs(os.Args), os.Stderr)
		// exitError carries a command's own exit code (e.g. gap-report 1/2). An empty
		// msg means the command already wrote its output; only the code propagates.
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+provenance.Sanitize(ee.msg)))
			}
			os.Exit(ee.code)
		}
		// A --chain <name> that the chain file doesn't define is a usage error: exit 2
		// (distinct from the generic exit 1), with the available chain names already in
		// the error message (chain.UnknownChainError.Error()).
		var uce *chain.UnknownChainError
		if errors.As(err, &uce) {
			// The message lists chain-section names read from .cenvkit.envchain.
			_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+provenance.Sanitize(err.Error())))
			os.Exit(2)
		}
		_, _ = fmt.Fprintln(os.Stderr, es.ErrorMsg("cenvkit: "+provenance.Sanitize(err.Error())))
		os.Exit(1)
	}
}

// colorFlagFromArgs best-effort-extracts the --color value from raw args so error
// coloring honors --color even when cobra parsing failed before PersistentPreRunE.
// Defaults to "auto". Accepts `--color X` and `--color=X`.
func colorFlagFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case a == "--color":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--color="):
			return strings.TrimPrefix(a, "--color=")
		}
	}
	return "auto"
}
