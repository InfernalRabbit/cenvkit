package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Styler renders semantic elements with optional color/formatting. It is defined
// HERE (not in internal/style) so internal/provenance imports NO styling library —
// the lipgloss-backed implementation lives in internal/style and is injected via
// HumanOpts.Style. A nil Style falls back to plainStyler (byte-identical plain),
// so callers that don't set it (and the existing render tests) are unchanged.
//
// Methods either style a passed string (Header(s) → styled s) or, for the markers
// and arrow which have no payload, return the styled glyph (MarkerNew() → "+").
type Styler interface {
	Header(s string) string  // section headers — bold cyan
	MarkerNew() string       // "+" new — green
	MarkerOverride() string  // "~" override — yellow
	MarkerRepeat() string    // "·" repeat — dim
	Arrow() string           // "→" between old/new — dim
	Key(s string) string     // KEY name — bold
	Value(s string) string   // a value — normal/readable
	Old(s string) string     // the shadowed (old) value — dim
	Path(s string) string    // file path — cyan
	Service(s string) string // service name — bold magenta
	SourceLabel(s string) string
	Gap(s string) string     // gap line body — red
	GapName(s string) string // the gapped var name — bold red
	Hint(s string) string    // a dim advisory line (e.g. empty-chain hint) — dim
	Ok(s string) string      // validate ok — green
	Fail(s string) string    // validate fail — red
	Created(s string) string // init created — green
	Skipped(s string) string // init skipped — dim
	ErrorMsg(s string) string
}

// plainStyler is the nil-safe fallback: every method returns its arg unchanged,
// and the markers/arrow return their plain glyphs. This keeps human output
// byte-identical to the pre-color behavior whenever no Styler is injected.
type plainStyler struct{}

func (plainStyler) Header(s string) string      { return s }
func (plainStyler) MarkerNew() string           { return "+" }
func (plainStyler) MarkerOverride() string      { return "~" }
func (plainStyler) MarkerRepeat() string        { return "·" }
func (plainStyler) Arrow() string               { return "→" }
func (plainStyler) Key(s string) string         { return s }
func (plainStyler) Value(s string) string       { return s }
func (plainStyler) Old(s string) string         { return s }
func (plainStyler) Path(s string) string        { return s }
func (plainStyler) Service(s string) string     { return s }
func (plainStyler) SourceLabel(s string) string { return s }
func (plainStyler) Gap(s string) string         { return s }
func (plainStyler) GapName(s string) string     { return s }
func (plainStyler) Hint(s string) string        { return s }
func (plainStyler) Ok(s string) string          { return s }
func (plainStyler) Fail(s string) string        { return s }
func (plainStyler) Created(s string) string     { return s }
func (plainStyler) Skipped(s string) string     { return s }
func (plainStyler) ErrorMsg(s string) string    { return s }

// Sanitize strips terminal-driving control characters from s: C0 controls
// (except TAB, which keeps column alignment), DEL, and the C1 range (0x80–0x9F,
// which carries the 8-bit CSI/OSC introducers). Human output embeds values, file
// paths, service names and chain-section names that may come from a repository's
// env files or .cenvkit.envchain — a raw ESC at those points starts an ANSI/OSC
// escape sequence, and a raw CR/LF forges extra lines (CWE-150). The JSON path is
// deliberately NOT sanitized: encoding/json already escapes control characters,
// and machine consumers must receive the true value.
func Sanitize(s string) string {
	clean := true
	for _, r := range s {
		if isUnsafeControl(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isUnsafeControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUnsafeControl(r rune) bool {
	switch {
	case r == '	':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	}
	return false
}

// sanitizingStyler is a Styler decorator that Sanitizes every payload before
// handing it to the real styler. Applying it once in st() covers every rendered
// value/key/path/service/gap string, so no call site has to remember.
type sanitizingStyler struct{ inner Styler }

func (s sanitizingStyler) Header(v string) string      { return s.inner.Header(Sanitize(v)) }
func (s sanitizingStyler) MarkerNew() string           { return s.inner.MarkerNew() }
func (s sanitizingStyler) MarkerOverride() string      { return s.inner.MarkerOverride() }
func (s sanitizingStyler) MarkerRepeat() string        { return s.inner.MarkerRepeat() }
func (s sanitizingStyler) Arrow() string               { return s.inner.Arrow() }
func (s sanitizingStyler) Key(v string) string         { return s.inner.Key(Sanitize(v)) }
func (s sanitizingStyler) Value(v string) string       { return s.inner.Value(Sanitize(v)) }
func (s sanitizingStyler) Old(v string) string         { return s.inner.Old(Sanitize(v)) }
func (s sanitizingStyler) Path(v string) string        { return s.inner.Path(Sanitize(v)) }
func (s sanitizingStyler) Service(v string) string     { return s.inner.Service(Sanitize(v)) }
func (s sanitizingStyler) SourceLabel(v string) string { return s.inner.SourceLabel(Sanitize(v)) }
func (s sanitizingStyler) Gap(v string) string         { return s.inner.Gap(Sanitize(v)) }
func (s sanitizingStyler) GapName(v string) string     { return s.inner.GapName(Sanitize(v)) }
func (s sanitizingStyler) Hint(v string) string        { return s.inner.Hint(Sanitize(v)) }
func (s sanitizingStyler) Ok(v string) string          { return s.inner.Ok(Sanitize(v)) }
func (s sanitizingStyler) Fail(v string) string        { return s.inner.Fail(Sanitize(v)) }
func (s sanitizingStyler) Created(v string) string     { return s.inner.Created(Sanitize(v)) }
func (s sanitizingStyler) Skipped(v string) string     { return s.inner.Skipped(Sanitize(v)) }
func (s sanitizingStyler) ErrorMsg(v string) string    { return s.inner.ErrorMsg(Sanitize(v)) }

// redactingStyler masks VALUE payloads for --redact. Keys, paths, service names,
// section headers and markers stay visible, so the shape of the environment is
// still readable while no secret leaves the process.
type redactingStyler struct{ inner Styler }

func (s redactingStyler) Header(v string) string      { return s.inner.Header(v) }
func (s redactingStyler) MarkerNew() string           { return s.inner.MarkerNew() }
func (s redactingStyler) MarkerOverride() string      { return s.inner.MarkerOverride() }
func (s redactingStyler) MarkerRepeat() string        { return s.inner.MarkerRepeat() }
func (s redactingStyler) Arrow() string               { return s.inner.Arrow() }
func (s redactingStyler) Key(v string) string         { return s.inner.Key(v) }
func (s redactingStyler) Value(string) string         { return "<redacted>" }
func (s redactingStyler) Old(string) string           { return "<redacted>" }
func (s redactingStyler) Path(v string) string        { return s.inner.Path(v) }
func (s redactingStyler) Service(v string) string     { return s.inner.Service(v) }
func (s redactingStyler) SourceLabel(v string) string { return s.inner.SourceLabel(v) }
func (s redactingStyler) Gap(v string) string         { return s.inner.Gap(v) }
func (s redactingStyler) GapName(v string) string     { return s.inner.GapName(v) }
func (s redactingStyler) Hint(v string) string        { return s.inner.Hint(v) }
func (s redactingStyler) Ok(v string) string          { return s.inner.Ok(v) }
func (s redactingStyler) Fail(v string) string        { return s.inner.Fail(v) }
func (s redactingStyler) Created(v string) string     { return s.inner.Created(v) }
func (s redactingStyler) Skipped(v string) string     { return s.inner.Skipped(v) }
func (s redactingStyler) ErrorMsg(v string) string    { return s.inner.ErrorMsg(v) }

// st returns the injected Styler wrapped in the sanitizing decorator, or the
// decorator around plainStyler when none was set (nil-safe).
// decorator around plainStyler when none was set (nil-safe).
func st(s Styler) Styler {
	if s == nil {
		return sanitizingStyler{plainStyler{}}
	}
	return sanitizingStyler{s}
}

// HumanOpts selects which view RenderHuman emits. Exactly one mode is active at a
// time; the switch in RenderHuman picks the first set field in precedence order.
type HumanOpts struct {
	Trace     string // non-empty: single-var trace (A + B-lite)
	Effective bool   // per-service env (C)
	Service   string // filter Effective to one service
	FilterVar string // filter Effective to one KEY (the --var value)
	Views     []View // explicit cascade order (--show); empty => derive from the legacy bools
	Redact    bool   // mask values as <redacted>
	Chain     bool   // Layer-1-only list (default view) — renders Report.ChainFiles
	Files     bool   // two-group view: interpolation (Report.Files) + runtime-only (Report.Services)
	Value     string // non-empty: print the winning value only
	Overview  bool   // per-file layering overview (raw values, +/~/· markers) — renders Report.Layers

	// Header inputs for the --overview mode (best-effort; from chain.Resolve + cmd).
	ComposeEnv       string // resolved CENVKIT_ENV value
	ComposeEnvSource string // where it came from: "shell" | ".env" | "default"
	ProjectDir       string // project dir (basename used as the overview title)

	Style Styler // optional color/formatting; nil ⇒ plain (byte-identical)
}

// JSONOpts narrows the machine-readable report. The zero value renders the whole
// report (the historical behaviour, so existing consumers keep working).
type JSONOpts struct {
	Service    string // keep only this service block
	Var        string // keep only this variable trace and the service entries for it
	Views      []View // non-empty: keep only the sections the requested views need
	Redact     bool   // mask every value as <redacted>
	DropLayers bool   // the raw layer dump was built only to filter; do not emit it
}

// RenderJSON writes the whole Report as indented JSON.
func RenderJSON(w io.Writer, r Report) error { return RenderJSONOpts(w, r, JSONOpts{}) }

// RenderJSONOpts writes the Report as indented JSON with the optional
// service/variable filters applied to a copy, so the caller keeps its Report.
func RenderJSONOpts(w io.Writer, r Report, o JSONOpts) error {
	if o.Service != "" {
		kept := make([]ServiceEnv, 0, 1)
		for _, se := range r.Services {
			if se.Service == o.Service {
				kept = append(kept, se)
			}
		}
		r.Services = kept
	}
	if o.Var != "" {
		if vt, ok := r.Vars[o.Var]; ok {
			r.Vars = map[string]VarTrace{o.Var: vt}
		} else {
			r.Vars = map[string]VarTrace{}
		}
		keptServices := make([]ServiceEnv, 0, len(r.Services))
		for _, se := range r.Services {
			keptEntries := make([]EnvEntry, 0, 1)
			for _, e := range se.Entries {
				if e.Key == o.Var {
					keptEntries = append(keptEntries, e)
				}
			}
			if len(keptEntries) > 0 {
				se.Entries = keptEntries
				keptServices = append(keptServices, se)
			}
		}
		r.Services = keptServices
		// Narrow the file lists too when the raw layers are available (the command
		// requests them for this combination), so the document answers "WHERE".
		if len(r.Layers) > 0 {
			declaring := filesDeclaring(r, o.Var)
			r.ChainFiles = declaring
			r.Files = append([]string(nil), declaring...)
		}
	}
	if o.Redact {
		r = redactReport(r)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if o.DropLayers {
		// The layer dump was built only so the --var filter could answer "where";
		// it is internal to --show overview and must not leak into other views.
		r.Layers = nil
	}
	if len(o.Views) == 0 {
		return enc.Encode(r)
	}
	doc := jsonDoc{}
	for _, v := range o.Views {
		switch v {
		case ViewList:
			doc.ChainFiles = &r.ChainFiles
		case ViewFiles:
			doc.Files = &r.Files
			doc.Services = &r.Services
		case ViewEffective, ViewTrace:
			doc.Vars = &r.Vars
			doc.Services = &r.Services
		case ViewOverview:
			doc.Layers = &r.Layers
		}
	}
	return enc.Encode(doc)
}

// jsonDoc is the section-selective document emitted when --show is given. Pointer
// fields keep unrequested sections out while still emitting requested-but-empty
// ones (as [] or {}), so consumers can rely on the shape.
type jsonDoc struct {
	Files      *[]string            `json:"files,omitempty"`
	ChainFiles *[]string            `json:"chain_files,omitempty"`
	Vars       *map[string]VarTrace `json:"vars,omitempty"`
	Services   *[]ServiceEnv        `json:"services,omitempty"`
	Layers     *[]OverviewLayer     `json:"layers,omitempty"`
}

// redactReport returns a copy of r with every rendered value masked. Structure,
// keys, sources and layer membership are preserved so the document stays useful.
func redactReport(r Report) Report {
	const masked = "<redacted>"
	vars := make(map[string]VarTrace, len(r.Vars))
	for k, vt := range r.Vars {
		vt.Value = masked
		for i := range vt.Effects {
			vt.Effects[i].Resolved = masked
		}
		for i := range vt.RuntimeDefs {
			vt.RuntimeDefs[i].Value = masked
		}
		vars[k] = vt
	}
	r.Vars = vars
	services := make([]ServiceEnv, len(r.Services))
	for i, se := range r.Services {
		entries := make([]EnvEntry, len(se.Entries))
		copy(entries, se.Entries)
		for j := range entries {
			entries[j].Value = masked
		}
		se.Entries = entries
		services[i] = se
	}
	r.Services = services
	layers := make([]OverviewLayer, len(r.Layers))
	for i, l := range r.Layers {
		entries := make([]OverviewEntry, len(l.Entries))
		copy(entries, l.Entries)
		for j := range entries {
			entries[j].RawValue = masked
		}
		l.Entries = entries
		layers[i] = l
	}
	r.Layers = layers
	return r
}

// RenderHuman writes the selected view as plain (or styled) text. The styler is
// resolved once (nil ⇒ plain) and threaded into each view.
// View names one stage of the human output. Stages are CASCADING: when several are
// requested they render one after another in the canonical order below, separated by
// a blank line, instead of the old winner-takes-all precedence.
type View string

const (
	ViewList      View = "list"
	ViewFiles     View = "files"
	ViewOverview  View = "overview"
	ViewEffective View = "effective"
	ViewTrace     View = "trace"
)

// ActiveViews returns the requested stages in cascade order.
func (o HumanOpts) ActiveViews() []View {
	if len(o.Views) > 0 {
		return o.Views
	}
	views := make([]View, 0, 5)
	if o.Chain {
		views = append(views, ViewList)
	}
	if o.Files {
		views = append(views, ViewFiles)
	}
	if o.Overview {
		views = append(views, ViewOverview)
	}
	if o.Effective {
		views = append(views, ViewEffective)
	}
	if o.Trace != "" {
		views = append(views, ViewTrace)
	}
	return views
}

// RenderHuman writes every requested stage, in cascade order. --value is the one
// non-cascading output (a bare value for scripts); the command layer rejects it
// together with other stages, so it stays a single early return here.
func RenderHuman(w io.Writer, r Report, o HumanOpts) {
	s := st(o.Style)
	if o.Redact {
		s = redactingStyler{inner: s}
	}
	if o.Value != "" {
		if o.Redact {
			_, _ = fmt.Fprintln(w, "<redacted>")
			return
		}
		_, _ = fmt.Fprintln(w, Sanitize(r.Vars[o.Value].Value))
		return
	}
	views := o.ActiveViews()
	if len(views) == 0 {
		views = []View{ViewList} // bare invocation: the Layer-1 chain list
	}
	for i, v := range views {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		switch v {
		case ViewList:
			renderList(w, r, o, s)
		case ViewFiles:
			renderFiles(w, r, o, s)
		case ViewOverview:
			renderOverview(w, r, o, s)
		case ViewEffective:
			renderEffective(w, r, o.Service, o.FilterVar, s)
		case ViewTrace:
			renderTrace(w, r, o.Trace, o.Service, o.Redact, s)
		}
	}
}

// renderList prints the Layer-1 chain files, narrowed by --var to the files that
// actually declare that key (requires Report.Layers, which the command requests
// when a variable filter is combined with --list/--files).
func renderList(w io.Writer, r Report, o HumanOpts, s Styler) {
	files := r.ChainFiles
	if o.FilterVar != "" {
		files = filesDeclaring(r, o.FilterVar)
	}
	if len(files) == 0 {
		if o.FilterVar != "" {
			_, _ = fmt.Fprintf(w, "%s\n", s.Hint(fmt.Sprintf("%s: declared in no chain file", Sanitize(o.FilterVar))))
			return
		}
		_, _ = fmt.Fprintln(w, s.Hint(emptyChainHint))
		return
	}
	for _, f := range files {
		_, _ = fmt.Fprintln(w, s.Path(f))
	}
}

// filterKeyEntries returns entries unchanged when key is empty, else only the ones
// declaring that key. It backs the --var filter of every view.
func filterKeyEntries(entries []OverviewEntry, key string) []OverviewEntry {
	if key == "" {
		return entries
	}
	out := make([]OverviewEntry, 0, 1)
	for _, e := range entries {
		if e.Key == key {
			out = append(out, e)
		}
	}
	return out
}

// filesDeclaring returns the Layer-1 files whose raw entries declare key.
func filesDeclaring(r Report, key string) []string {
	var out []string
	for _, l := range r.Layers {
		if l.Layer != "layer1" {
			continue
		}
		if len(filterKeyEntries(l.Entries, key)) > 0 {
			out = append(out, l.File)
		}
	}
	return out
}

// serviceFilesDeclaring returns the env_file: paths of one service that declare key.
func serviceFilesDeclaring(r Report, service, key string) []string {
	var out []string
	for _, l := range r.Layers {
		if l.Service != service || l.Layer != "env_file" {
			continue
		}
		if len(filterKeyEntries(l.Entries, key)) > 0 {
			out = append(out, l.File)
		}
	}
	return out
}

// emptyChainHint is the dim advisory shown in the human env-debug views when the
// Layer-1 interpolation chain has ZERO existing files — otherwise those views print
// an empty section that reads as broken (FIX 1). Kept neutral (does not assume
// example.* exist). NOT emitted by the `env-files` command (machine output: an
// empty stdout is correct for piping).
const emptyChainHint = "(none — no Layer-1 chain files present; run `cenvkit init` or add .env)"

// renderTrace prints one variable's story. Post-v3 it is gap-aware: a var present
// in the interpolation (Layer-1) chain renders the normal winner/overridden/effects
// view; a var defined only in a service env_file: (a gap) renders the
// interpolation/runtime split + a ⚠ gap line per effect + a fix hint, explaining
// that the env_file value is container-only and never reaches the run's ${VAR}.
func renderTrace(w io.Writer, r Report, name, service string, redact bool, s Styler) {
	vt, ok := r.Vars[name]
	if !ok {
		// Not an interpolation variable at all. It may still be a plain key of a
		// service env_file: (runtime-only) — the case that used to answer "not set"
		// and send people grepping the repo. Explain where it actually lives.
		type runtimeRow struct {
			service string
			entry   EnvEntry
		}
		var rows []runtimeRow
		for _, se := range r.Services {
			if service != "" && se.Service != service {
				continue
			}
			for _, e := range se.Entries {
				if e.Key == name {
					rows = append(rows, runtimeRow{service: se.Service, entry: e})
				}
			}
		}
		if len(rows) == 0 {
			_, _ = fmt.Fprintf(w, "%s: not set\n", Sanitize(name))
			return
		}
		_, _ = fmt.Fprintln(w, s.Key(name))
		_, _ = fmt.Fprintf(w, "  interpolation: not referenced as $%s anywhere in the compose model\n", "{"+Sanitize(name)+"}")
		for _, rd := range rows {
			_, _ = fmt.Fprintf(w, "  runtime-only:  %s -> %s=%s  (service %s container env only)\n",
				s.Path(rd.entry.Source.File), Sanitize(name), s.Value(rd.entry.Value), s.Service(rd.service))
		}
		_, _ = fmt.Fprintln(w, "  note:          delivered to the container; it is not an interpolation variable")
		return
	}
	if vt.InChain {
		// Normal path: the var resolves from the Layer-1 chain (or shell) — no gap.
		_, _ = fmt.Fprintf(w, "%s=%s\n", s.Key(name), s.Value(vt.Value))
		_, _ = fmt.Fprintf(w, "  winner:     %s (%s)\n", s.Path(vt.Winner.File), s.SourceLabel(vt.Winner.Layer))
		for _, src := range vt.Overridden {
			_, _ = fmt.Fprintf(w, "  overridden: %s (%s)\n", s.Path(src.File), s.SourceLabel(src.Layer))
		}
		for _, e := range vt.Effects {
			if service != "" && e.Service != service {
				continue
			}
			_, _ = fmt.Fprintf(w, "  effect:     service %s field %s -> %s\n", s.Service(e.Service), Sanitize(e.Field), s.Value(stripVarPrefix(name, e.Resolved)))
		}
		return
	}
	// Not in the interpolation chain: show the interpolation/runtime split.
	_, _ = fmt.Fprintln(w, s.Key(name))
	_, _ = fmt.Fprintln(w, "  interpolation: NOT in the Layer-1 chain -> ${"+Sanitize(name)+"} falls back at run time")
	for _, rd := range vt.RuntimeDefs {
		if service != "" && rd.Service != service {
			continue
		}
		_, _ = fmt.Fprintf(w, "  runtime:       %s -> %s=%s  (service `%s` container env only)\n",
			s.Path(rd.File), Sanitize(name), s.Value(rd.Value), s.Service(rd.Service))
	}
	if vt.Gap {
		for _, e := range vt.Effects {
			if service != "" && e.Service != service {
				continue
			}
			resolved := stripVarPrefix(name, e.Resolved)
			if redact {
				resolved = "<redacted>"
			}
			_, _ = fmt.Fprintf(w, "  %s\n", s.Gap(fmt.Sprintf("⚠ gap: ${%s} used in service %s %s resolves to %q at the run, NOT the env_file value (defined only in a service env_file).",
				name, e.Service, e.Field, resolved)))
		}
		_, _ = fmt.Fprintf(w, "  fix:   add %s to the Layer-1 chain (e.g. .env), or use it runtime-only.\n", Sanitize(name))
	}
}

// renderFiles prints the two labeled groups (D2 / spec §3.4): the interpolation
// set (COMPOSE_ENV_FILES = Layer 1) and the runtime-only set (service env_file:
// paths grouped by service, NOT interpolated — container env only).
func renderFiles(w io.Writer, r Report, o HumanOpts, s Styler) {
	interp := r.Files
	if o.FilterVar != "" {
		interp = filesDeclaring(r, o.FilterVar)
	}
	_, _ = fmt.Fprintln(w, s.Header("interpolation (COMPOSE_ENV_FILES):"))
	if len(interp) == 0 {
		if o.FilterVar != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", s.Hint(fmt.Sprintf("%s: declared in no chain file", Sanitize(o.FilterVar))))
		} else {
			_, _ = fmt.Fprintf(w, "  %s\n", s.Hint(emptyChainHint)) // FIX 1
		}
	}
	for _, f := range interp {
		_, _ = fmt.Fprintf(w, "  %s\n", s.Path(f))
	}
	_, _ = fmt.Fprintln(w, s.Header("runtime-only (service env_file: — NOT interpolated, container env only):"))
	printed := 0
	for _, se := range r.Services {
		if o.Service != "" && se.Service != o.Service {
			continue
		}
		// Render the DECLARED env_file: paths (ServiceEnv.EnvFiles), not the per-key
		// Entries sources — a file whose every key is inline-overridden still appears
		// here (N-3). With --var keep only the files that declare that key.
		files := se.EnvFiles
		if o.FilterVar != "" {
			files = serviceFilesDeclaring(r, se.Service, o.FilterVar)
		}
		if len(files) == 0 {
			continue
		}
		printed++
		_, _ = fmt.Fprintf(w, "  %s:\n", s.Service(se.Service))
		for _, f := range files {
			_, _ = fmt.Fprintf(w, "    %s\n", s.Path(f))
		}
	}
	if printed == 0 && o.FilterVar != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", s.Hint(fmt.Sprintf("%s: declared in no service env_file:", Sanitize(o.FilterVar))))
	}
}

// renderOverview prints the two-section layering overview (spec §2/§3): a header,
// then the interpolation chain (Layer-1) with a single accumulator, then the
// runtime-only service env_file: layers with a FRESH accumulator per service and
// an `inline environment:` pseudo-layer last, plus a `⚠ gap:` line per service for
// any var that is referenced as ${VAR} but defined only in that service's
// env_file:. Markers are derived here from the accumulator walk (not stored):
//
//   - new        the key is first defined in this layer
//     ~ override   an earlier layer set it → show `old → new`
//     · repeat     set again to the same value
func renderOverview(w io.Writer, r Report, o HumanOpts, s Styler) {
	// Header (best-effort, like the sh kit).
	title := filepath.Base(o.ProjectDir)
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = o.ProjectDir
	}
	_, _ = fmt.Fprintln(w, s.Header(fmt.Sprintf("env overview — %s (mode: overview)", title)))
	if o.ComposeEnv != "" {
		if o.ComposeEnvSource != "" {
			_, _ = fmt.Fprintf(w, "  CENVKIT_ENV = %s (from %s)\n", s.Value(o.ComposeEnv), s.SourceLabel(o.ComposeEnvSource))
		} else {
			_, _ = fmt.Fprintf(w, "  CENVKIT_ENV = %s\n", s.Value(o.ComposeEnv))
		}
	}
	if o.ProjectDir != "" {
		_, _ = fmt.Fprintf(w, "  Project dir = %s\n", s.Path(o.ProjectDir))
	}

	// Section 1 — interpolation chain (Layer-1 only), narrowed by --var to the
	// layers that actually declare that key.
	_, _ = fmt.Fprintln(w, "\n"+s.Header("Interpolation chain (COMPOSE_ENV_FILES)"))
	_, _ = fmt.Fprintf(w, "  %s new   %s override   %s repeat\n", s.MarkerNew(), s.MarkerOverride(), s.MarkerRepeat())
	chainAcc := map[string]string{}
	chainLayers := 0
	for _, l := range r.Layers {
		if l.Layer != "layer1" {
			continue
		}
		entries := filterKeyEntries(l.Entries, o.FilterVar)
		if len(entries) == 0 {
			continue
		}
		chainLayers++
		_, _ = fmt.Fprintf(w, "\n  %s\n", s.Path(l.File))
		renderLayerEntries(w, entries, chainAcc, s)
	}
	if chainLayers == 0 {
		if o.FilterVar != "" {
			_, _ = fmt.Fprintf(w, "\n  %s\n", s.Hint(fmt.Sprintf("%s: not defined in any interpolation-chain file", Sanitize(o.FilterVar))))
		} else {
			_, _ = fmt.Fprintf(w, "\n  %s\n", s.Hint(emptyChainHint)) // FIX 1
		}
	}

	// Section 2 — runtime-only, per service (fresh accumulator each). With --var
	// only the layers declaring that key survive; --service narrows to one service;
	// the section header is suppressed when nothing is left (N-4).
	shown := 0
	serviceNames := make([]string, 0, len(r.Services))
	for _, name := range overviewServices(r.Layers) {
		if o.Service != "" && name != o.Service {
			continue
		}
		for _, l := range r.Layers {
			if l.Service != name {
				continue
			}
			if len(filterKeyEntries(l.Entries, o.FilterVar)) == 0 {
				continue
			}
			serviceNames = append(serviceNames, name)
			shown++
			break
		}
	}
	if shown == 0 {
		if o.FilterVar != "" {
			_, _ = fmt.Fprintf(w, "\n  %s\n", s.Hint(fmt.Sprintf("%s: not declared in any service env_file: or inline environment:", Sanitize(o.FilterVar))))
		}
		return
	}
	_, _ = fmt.Fprintln(w, "\n"+s.Header("Runtime-only — service env_file: (NOT interpolated)"))
	for _, name := range serviceNames {
		_, _ = fmt.Fprintf(w, "  %s:\n", s.Service(name))
		svcAcc := map[string]string{}
		for _, l := range r.Layers {
			if l.Service != name {
				continue
			}
			entries := filterKeyEntries(l.Entries, o.FilterVar)
			if len(entries) == 0 {
				continue
			}
			label := l.File
			if l.Layer == "environment" {
				label = "inline environment:"
			}
			_, _ = fmt.Fprintf(w, "    %s\n", s.Path(label))
			renderLayerEntriesIndented(w, entries, svcAcc, s)
		}
		renderServiceGaps(w, r, name, s, o.FilterVar)
	}
}

// renderLayerEntries prints one chain layer's entries with +/~/· markers against
// acc (6-space indent for chain entries), updating acc as it goes.
func renderLayerEntries(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler) {
	renderEntriesAt(w, entries, acc, s, "      ")
}

// renderLayerEntriesIndented is renderLayerEntries with the deeper runtime-section
// indent (per the spec example: service > layer > entry).
func renderLayerEntriesIndented(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler) {
	renderEntriesAt(w, entries, acc, s, "        ")
}

// renderEntriesAt walks entries applying +/~/· markers (styled) and the key/value/
// old/arrow styling at the given indent. Marker semantics are identical to before;
// only the rendered glyph/value strings pass through the styler (plain ⇒ identical).
func renderEntriesAt(w io.Writer, entries []OverviewEntry, acc map[string]string, s Styler, indent string) {
	for _, e := range entries {
		kind, old, seen := classify(acc, e)
		key := s.Key(e.Key)
		switch {
		case !seen:
			_, _ = fmt.Fprintf(w, "%s%s %s = %s\n", indent, s.MarkerNew(), key, s.Value(e.RawValue))
		case kind == "~":
			_, _ = fmt.Fprintf(w, "%s%s %s = %s %s %s\n", indent, s.MarkerOverride(), key, s.Old(old), s.Arrow(), s.Value(e.RawValue))
		default: // ·
			_, _ = fmt.Fprintf(w, "%s%s %s = %s\n", indent, s.MarkerRepeat(), key, s.Value(e.RawValue))
		}
		acc[e.Key] = e.RawValue
	}
}

// classify returns the marker KIND for an entry against the accumulator: "+" when
// the key is unseen, "~" when seen with a different value (old = the prior value),
// "·" when seen with the same value. (The styled glyph is chosen by the caller.)
func classify(acc map[string]string, e OverviewEntry) (kind, old string, seen bool) {
	prev, ok := acc[e.Key]
	if !ok {
		return "+", "", false
	}
	if prev != e.RawValue {
		return "~", prev, true
	}
	return "·", prev, true
}

// overviewServices returns the distinct service names present in the runtime
// layers, in first-seen order (the engine emits services in sorted order).
func overviewServices(layers []OverviewLayer) []string {
	seen := map[string]bool{}
	var names []string
	for _, l := range layers {
		if l.Service == "" || seen[l.Service] {
			continue
		}
		seen[l.Service] = true
		names = append(names, l.Service)
	}
	return names
}

// renderServiceGaps prints a `⚠ gap:` line per var that is a v3 gap (referenced as
// ${VAR} but env_file-only) AND referenced in THIS service — reusing the existing
// gap detection (Vars[].Gap / Effects), pure presentation.
func renderServiceGaps(w io.Writer, r Report, service string, s Styler, filterVar string) {
	names := make([]string, 0, len(r.Vars))
	for name := range r.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if filterVar != "" && name != filterVar {
			continue
		}
		vt := r.Vars[name]
		if !vt.Gap {
			continue
		}
		var fields []string
		fallback := ""
		for _, e := range vt.Effects {
			if e.Service == service {
				fields = append(fields, e.Field)
				if fallback == "" {
					fallback = stripVarPrefix(name, e.Resolved) // run's fallback, normalized (FIX 2)
				}
			}
		}
		if len(fields) == 0 {
			continue // gap is in another service
		}
		// The whole gap line is red; the gapped var name is bold red within it.
		_, _ = fmt.Fprintf(w, "    %s\n", s.Gap(fmt.Sprintf("⚠ gap: %s — used as ${%s} in service %s (%s)",
			s.GapName(name), name, service, strings.Join(fields, ", "))))
		_, _ = fmt.Fprintf(w, "    %s\n", s.Gap(fmt.Sprintf("  but NOT in the Layer-1 chain → run falls back to %q.", fallback)))
	}
}

// stripVarPrefix normalizes a resolved value for HUMAN display: a list-form
// `environment: ["NAME=${NAME:-0}"]` entry resolves to the whole "NAME=0" (the leaf
// includes the key), while map-form `environment: {NAME: ...}` resolves to bare
// "0". Strip a leading "NAME=" so both render the same value (FIX 2). Only the
// exact "<name>=" prefix is stripped — a non-environment leaf like ports "0:80"
// (no "<name>=") is left untouched. The stored Report.Effect.Resolved keeps the raw
// value (for --json); this strip is render-time only.
func stripVarPrefix(name, resolved string) string {
	if p := name + "="; strings.HasPrefix(resolved, p) {
		return resolved[len(p):]
	}
	return resolved
}

// renderEffective prints each service's effective env. Entries are pre-sorted by
// the engine (deterministic emit), so we iterate as-is.
func renderEffective(w io.Writer, r Report, service, filterVar string, s Styler) {
	printed := 0
	for _, se := range r.Services {
		if service != "" && se.Service != service {
			continue
		}
		var matched []EnvEntry
		for _, e := range se.Entries {
			if filterVar == "" || e.Key == filterVar {
				matched = append(matched, e)
			}
		}
		if len(matched) == 0 {
			continue // the key lives only in other services
		}
		_, _ = fmt.Fprintf(w, "service %s:\n", s.Service(se.Service))
		for _, e := range matched {
			_, _ = fmt.Fprintf(w, "  %s=%s\t<- %s (%s)\n",
				s.Key(e.Key), s.Value(e.Value), s.Path(e.Source.File), s.SourceLabel(e.Source.Layer))
		}
		printed += len(matched)
	}
	if printed == 0 && filterVar != "" {
		_, _ = fmt.Fprintf(w, "%s: no service exposes this key in its effective env\n", Sanitize(filterVar))
	}
}
