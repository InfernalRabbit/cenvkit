package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/InfernalRabbit/cenvkit/internal/provenance"
)

// ProvFile is one ordered COMPOSE_ENV_FILES entry tagged with its chain layer.
type ProvFile struct{ Path, Layer string } // Layer: "layer1" | "layer2"

// ProvInput keeps process environment separate from the ordered project chain.
// Env must not contain chain.Result.Vars: those include unexpanded file seeds.
type ProvInput struct {
	ProjectDir     string
	ConfigFiles    []string // explicit -f; empty => resolveComposeFiles
	Env            []string // process environment only; shell wins over chain files
	Profiles       []string
	EnvFiles       []ProvFile // ordered merged COMPOSE_ENV_FILES (for A attribution)
	WantLayers     bool       // populate Report.Layers (the raw ordered --overview lens); off by default to keep other modes' JSON unchanged (D-A)
	WantDeps       bool       // populate Report.Refs (interpolation edges for --show deps); implies layers
	RequireCompose bool       // gap lint must not report clean when no default config exists
}

// ParseOrderedLiteral reads a dotenv-style file into ORDERED, LITERAL entries for
// the --overview lens AND the populator's --no-expand path (internal/envmap folds
// the ordered entries into a last-wins map). compose-go's dotenv parser cannot be
// reused here: it returns an unordered map AND expands ${...} (verified v2.17.0,
// plan §0). This is a thin stdlib-only line reader (keeps the engine seam — no
// extra compose-go surface). It mirrors dotenv's KEY tokenization (skip
// blank/#-comment lines; strip a leading `export `; split on the first `=`; key
// charset [A-Za-z0-9_.-]) but takes the VALUE verbatim — NO ${...} expansion and NO
// escape processing — except it strips one matching surrounding quote pair, and for
// an UNQUOTED value trims a trailing " # comment" + trailing space (mirrors dotenv
// parser.go:157-159; decision D-B).
func ParseOrderedLiteral(path string) ([]provenance.OverviewEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var entries []provenance.OverviewEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// strip a leading `export` + following whitespace (mirror dotenv ^export\s+:
		// tabs too, N-1) — only when a space/tab actually follows, never "exportFOO".
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimLeft(rest, " \t")
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue // no key, or "=value" — skip (parity with a missing key name)
		}
		key := strings.TrimSpace(line[:i])
		if key == "" || !validEnvKey(key) {
			continue
		}
		raw := line[i+1:]                   // value as-read, leading space intact (dotenv-parity for the # cut)
		val := strings.TrimLeft(raw, " \t") // for quote detection
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1] // quoted: strip ONE matching pair, keep contents verbatim
		} else {
			// unquoted: cut an inline " # comment" on the RAW slice (so a value that
			// is ONLY a trailing comment, e.g. `KEY= # x`, becomes "" — SF-3), then
			// trim surrounding whitespace.
			if j := strings.Index(raw, " #"); j >= 0 {
				raw = raw[:j]
			}
			val = strings.TrimSpace(raw)
			// A quoted value followed by a trailing comment (KEY="v" # c) reaches
			// this branch because the pair is not at end-of-line. Now that the
			// comment is cut, strip ONE matching pair so the literal matches
			// compose-go's parser, which also cuts the comment before unquoting.
			if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
				val = val[1 : len(val)-1]
			}
		}
		entries = append(entries, provenance.OverviewEntry{Key: key, RawValue: val})
	}
	return entries, sc.Err()
}

// validEnvKey reports whether key matches dotenv's variable-name charset
// [A-Za-z0-9_.-] (mirrors parser.go:122-127). Rejects whitespace/`$`/etc., so a
// malformed line is skipped rather than rendered as a bogus entry.
func validEnvKey(key string) bool {
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if i := indexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// Provenance assembles A (chain attribution), B-lite (${VAR} effects) and C
// (per-service env with sources) into a single provenance.Report, fully in
// process (no docker daemon). compose-go work is confined to this package.
func (e *composeEngine) Provenance(ctx context.Context, in ProvInput) (provenance.Report, error) {
	rep := provenance.Report{Vars: map[string]provenance.VarTrace{}}
	shellMap := envSliceToMap(in.Env)
	var layer1Files []string
	for _, f := range in.EnvFiles {
		if f.Layer == "layer1" {
			layer1Files = append(layer1Files, f.Path)
		}
	}
	// The authoritative expansion is shared with run/env. Service env_file values
	// are never added to this project-wide interpolation context.
	interpEnv, err := Flatten(shellMap, layer1Files, true)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance flatten layer-1: %w", err)
	}
	for k, v := range shellMap {
		interpEnv[k] = v
	}

	// Attribute each declaration using the same shell-first, sequential dotenv
	// lookup as Flatten. A changed expansion is not a shell override.
	fileValues := map[string]string{}
	shellLookup := dotenv.LookupFn(func(k string) (string, bool) { v, ok := shellMap[k]; return v, ok })
	for _, f := range in.EnvFiles {
		if f.Layer != "layer1" {
			continue // Layer-2 (service env_file:) is runtime-only — no chain attribution
		}
		rep.Files = append(rep.Files, f.Path)
		rep.ChainFiles = append(rep.ChainFiles, f.Path)
		m, err := parseEnvFile(f.Path, "", fileValues, shellLookup)
		if err != nil {
			return provenance.Report{}, fmt.Errorf("attribute chain file: %w", err)
		}
		if in.WantLayers { // raw, ordered entries for the overview lens
			if entries, lerr := ParseOrderedLiteral(f.Path); lerr == nil {
				rep.Layers = append(rep.Layers, provenance.OverviewLayer{File: f.Path, Layer: "layer1", Entries: entries})
			}
		}
		src := provenance.Source{File: f.Path, Layer: f.Layer}
		for k, v := range m {
			fileValues[k] = v
			vt := rep.Vars[k]
			vt.Name = k
			if vt.Winner.File != "" {
				vt.Overridden = append(vt.Overridden, vt.Winner)
			}
			vt.Winner, vt.Value = src, v
			rep.Vars[k] = vt
		}
	}

	// Shell wins even if its value happens to equal the last file declaration.
	envSrc := provenance.Source{File: "(environment)", Layer: "environment"}
	for _, k := range sortedKeys(shellMap) {
		v := shellMap[k]
		vt := rep.Vars[k]
		vt.Name = k
		if vt.Winner.File != "" {
			vt.Overridden = append(vt.Overridden, vt.Winner)
		}
		vt.Winner, vt.Value = envSrc, v
		rep.Vars[k] = vt
	}

	for k, vt := range rep.Vars {
		vt.Value = interpEnv[k]
		_, vt.InChain = interpEnv[k]
		rep.Vars[k] = vt
	}

	configs := in.ConfigFiles
	if len(configs) == 0 {
		configs, err = resolveComposeFiles(in.ProjectDir, types.Mapping(interpEnv).Values())
		if err != nil {
			return provenance.Report{}, fmt.Errorf("resolve compose files: %w", err)
		}
	}
	// Chain-only debugging is valid; gap lint requires a compose model.
	if len(configs) == 0 {
		if in.RequireCompose {
			return provenance.Report{}, ErrNoComposeFile
		}
		return rep, nil
	}

	// Load YAML against Layer 1 + shell only; then resolve each service's runtime
	// env files independently, keeping sources and missing-file diagnostics.
	mergedEnv := make([]string, 0, len(interpEnv))
	for _, k := range sortedKeys(interpEnv) {
		mergedEnv = append(mergedEnv, k+"="+interpEnv[k])
	}
	opts, err := cli.NewProjectOptions(configs,
		cli.WithWorkingDirectory(in.ProjectDir),
		cli.WithEnv(mergedEnv),
		cli.WithDefaultProfiles(in.Profiles...),
		cli.WithResolvedPaths(true),
		cli.WithInterpolation(true),
		cli.WithoutEnvironmentResolution, // REQUIRED for C separability (probe §6)
	)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance options: %w", err)
	}
	proj, err := opts.LoadProject(ctx)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance load: %w", err)
	}
	// runtimeDefs[V] = every service env_file: definition of V (gap evidence;
	// runtime-only — these values apply only inside the service's container, never
	// to interpolation). Built from the SAME dotenv reads the C-loop already does.
	runtimeDefs := map[string][]provenance.ServiceVal{}
	svcNames := make([]string, 0, len(proj.Services))
	for n := range proj.Services {
		svcNames = append(svcNames, n)
	}
	sort.Strings(svcNames)
	for _, name := range svcNames {
		svc := proj.Services[name]
		svc.Environment.Resolve(proj.Environment.Resolve)
		final := map[string]string{}
		source := map[string]provenance.Source{}
		serviceLookup := dotenv.LookupFn(func(k string) (string, bool) {
			if v, ok := proj.Environment.Resolve(k); ok {
				return v, true
			}
			if v := svc.Environment[k]; v != nil {
				return *v, true
			}
			return "", false
		})
		// Declared env_file: paths in declared order (deduped). Collected
		// independently of parse success / inline overrides so the --files
		// runtime-only group is faithful to what the service LOADS — a file whose
		// every key is later inline-overridden must still appear (N-3 fix). The
		// per-key Entries below may rewrite a source to "(inline environment:)", but
		// that must not erase the file from this declared list.
		var declaredFiles []string
		seenFile := map[string]bool{}
		for _, ef := range svc.EnvFiles {
			if !seenFile[ef.Path] {
				seenFile[ef.Path] = true
				declaredFiles = append(declaredFiles, ef.Path)
				if in.WantLayers { // --overview: one raw ordered layer per distinct declared env_file
					if entries, lerr := ParseOrderedLiteral(ef.Path); lerr == nil {
						rep.Layers = append(rep.Layers, provenance.OverviewLayer{File: ef.Path, Layer: "env_file", Service: name, Entries: entries})
					}
				}
			}
			m, perr := parseEnvFile(ef.Path, ef.Format, final, serviceLookup)
			if perr != nil {
				if errors.Is(perr, os.ErrNotExist) {
					continue // missing required files remain lenient in debug mode
				}
				return provenance.Report{}, fmt.Errorf("service %s env_file: %w", name, perr)
			}
			for k, v := range m {
				final[k] = v
				source[k] = provenance.Source{File: ef.Path, Layer: "env_file"}
				runtimeDefs[k] = append(runtimeDefs[k], provenance.ServiceVal{Service: name, File: ef.Path, Value: v})
			}
		}
		for k, vp := range svc.Environment { // map[string]*string; inline overrides
			if vp == nil {
				delete(final, k) // unresolved bare keys unset the runtime variable
				delete(source, k)
				continue
			}
			final[k] = *vp
			source[k] = provenance.Source{File: "(inline environment:)", Layer: "environment"}
		}
		// Inline environment is the final runtime layer; values have already been
		// interpolated by the YAML loader, unlike literal env_file overview entries.
		if in.WantLayers && len(svc.Environment) > 0 {
			inlineKeys := make([]string, 0, len(svc.Environment))
			for k := range svc.Environment {
				inlineKeys = append(inlineKeys, k)
			}
			sort.Strings(inlineKeys)
			inline := provenance.OverviewLayer{File: "(inline environment:)", Layer: "environment", Service: name}
			for _, k := range inlineKeys {
				val := ""
				if vp := svc.Environment[k]; vp != nil {
					val = *vp
				}
				inline.Entries = append(inline.Entries, provenance.OverviewEntry{Key: k, RawValue: val})
			}
			rep.Layers = append(rep.Layers, inline)
		}
		se := provenance.ServiceEnv{Service: name, EnvFiles: declaredFiles}
		for _, k := range sortedKeys(final) {
			se.Entries = append(se.Entries, provenance.EnvEntry{Key: k, Value: final[k], Source: source[k]})
		}
		rep.Services = append(rep.Services, se)
	}

	// --- B-lite: single RAW load + dict walk + per-leaf Substitute ---
	details, err := loader.LoadConfigFiles(ctx, configs, in.ProjectDir)
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw config: %w", err)
	}
	details.Environment = types.Mapping(interpEnv) // cosmetic (SkipInterpolation) but kept honest: L1+shell only
	rawDict, err := loader.LoadModelWithContext(ctx, *details, func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipValidation = true
		o.SkipConsistencyCheck = true
		o.SkipResolveEnvironment = true
	})
	if err != nil {
		return provenance.Report{}, fmt.Errorf("provenance raw load: %w", err)
	}
	walkDict(rawDict, "", func(path, leaf string) {
		svc, field, ok := splitServiceField(path)
		if !ok {
			return
		}
		if _, active := proj.Services[svc]; !active {
			return
		}
		// Record executed lookups, not syntactic variables in unselected nested
		// default/presence branches. Upstream still owns substitution semantics.
		vars := map[string]bool{}
		resolved, _ := template.SubstituteWithOptions(leaf, func(k string) (string, bool) {
			vars[k] = true
			v, ok := interpEnv[k]
			return v, ok
		}, template.WithoutLogging)
		for vn := range vars {
			vt := rep.Vars[vn]
			vt.Name = vn
			vt.Effects = append(vt.Effects, provenance.Effect{Service: svc, Field: field, Resolved: resolved})
			rep.Vars[vn] = vt
		}
	})
	// --- gap detection (pure Go set logic over the two env contexts) ---
	// For each var: InChain = resolvable from the interpolation (Layer-1 + shell)
	// env; RuntimeDefs = its service env_file: definitions; Gap when the var is
	// REFERENCED in a service field (has Effects), is NOT in the interpolation
	// chain, yet IS defined in some service env_file: — precisely the #3435
	// footprint (the value sits in an env_file but the run interpolates the
	// :-default). Effects each carry the same Gap so renderers/JSON see it per site.
	for k, vt := range rep.Vars {
		sort.Slice(vt.Effects, func(i, j int) bool { // deterministic effects order
			if vt.Effects[i].Service != vt.Effects[j].Service {
				return vt.Effects[i].Service < vt.Effects[j].Service
			}
			return vt.Effects[i].Field < vt.Effects[j].Field
		})
		_, vt.InChain = interpEnv[k]
		vt.RuntimeDefs = runtimeDefs[k]
		sort.Slice(vt.RuntimeDefs, func(i, j int) bool { // stable JSON
			if vt.RuntimeDefs[i].Service != vt.RuntimeDefs[j].Service {
				return vt.RuntimeDefs[i].Service < vt.RuntimeDefs[j].Service
			}
			return vt.RuntimeDefs[i].File < vt.RuntimeDefs[j].File
		})
		referenced := len(vt.Effects) > 0
		vt.Gap = referenced && !vt.InChain && len(vt.RuntimeDefs) > 0
		for i := range vt.Effects {
			vt.Effects[i].Gap = vt.Gap
		}
		rep.Vars[k] = vt
	}
	if in.WantDeps {
		// Every raw layer entry that embeds a reference contributes an edge. Kept
		// separate from Vars on purpose: this is the dependency graph, not the
		// attribution table (a key can be defined in several files).
		for _, layer := range rep.Layers {
			for _, e := range layer.Entries {
				for _, to := range ParseReferences(e.RawValue) {
					rep.Refs = append(rep.Refs, provenance.Ref{
						FromFile:    layer.File,
						FromKey:     e.Key,
						FromService: layer.Service,
						FromLayer:   layer.Layer,
						ToVar:       to,
					})
				}
			}
		}
	}
	return rep, nil
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// walkDict recurses map[string]any / []any and calls fn on every string leaf
// with a dotted+[i] path (e.g. "services.web.ports[0]"). Map keys are sorted for
// deterministic traversal.
func walkDict(node any, path string, fn func(path, leaf string)) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walkDict(v[k], p, fn)
		}
	case []any:
		for i, child := range v {
			walkDict(child, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	case string:
		fn(path, v)
	}
}

// splitServiceField turns "services.web.ports[0]" into ("web", "ports[0]", true).
// It INTENTIONALLY returns ok=false for non-service paths (top-level
// networks/volumes/configs/secrets/x-*); those ${VAR} effects are out of scope
// for B-lite (see spec §2/§8) and are deliberately dropped, not a bug. Such a
// var still appears in A (chain attribution) if it is a COMPOSE_ENV_FILES key;
// only its Effects entry is omitted.
func splitServiceField(path string) (service, field string, ok bool) {
	const p = "services."
	if len(path) <= len(p) || path[:len(p)] != p {
		return "", "", false
	}
	rest := path[len(p):]
	i := indexByte(rest, '.')
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
