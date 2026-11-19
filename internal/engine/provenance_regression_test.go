package engine_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/InfernalRabbit/cenvkit/internal/engine"
	"github.com/InfernalRabbit/cenvkit/internal/provenance"
)

func TestProvenance_ExpandedChainAttribution(t *testing.T) {
	dir := t.TempDir()
	a := writeF(t, dir, "a.env", "BASE=first\nVALUE=${BASE}/a\n")
	b := writeF(t, dir, "b.env", "BASE=second\nVALUE=${BASE}/b\n")
	for _, shell := range []bool{false, true} {
		env := []string{}
		want, source := "second/b", b
		if shell {
			env = []string{"VALUE=second/b", "BASE=shell"}
			source = "(environment)"
		}
		rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
			ProjectDir: dir, Env: env,
			EnvFiles: []engine.ProvFile{{Path: a, Layer: "layer1"}, {Path: b, Layer: "layer1"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		vt := rep.Vars["VALUE"]
		if vt.Value != want || vt.Winner.File != source {
			t.Fatalf("shell=%v: VALUE=%+v, want %q from %s", shell, vt, want, source)
		}
		wantOverridden := 1
		if shell {
			wantOverridden++
		}
		if len(vt.Overridden) != wantOverridden {
			t.Fatalf("shell=%v: overridden=%v", shell, vt.Overridden)
		}
	}
}

func TestProvenance_ConfigSelection(t *testing.T) {
	dir := t.TempDir()
	writeF(t, dir, "compose.yaml", "services:\n  app: {image: busybox, environment: {WINNER: yaml}}\n")
	writeF(t, dir, "compose.yml", "services:\n  app: {image: busybox, environment: {WINNER: yml}}\n")
	writeF(t, dir, "compose.override.yaml", "services:\n  app:\n    environment: {OVERLAY: present}\n")
	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{ProjectDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	app := findService(rep, "app")
	if entry(app, "WINNER").Value != "yaml" || entry(app, "OVERLAY").Value != "present" {
		t.Fatalf("default discovery did not select base + override: %+v", app)
	}
	_, err = engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir, Env: []string{"COMPOSE_FILE=compose.yaml" + string(filepath.ListSeparator) + "missing.yaml"},
	})
	if err == nil {
		t.Fatal("missing explicit overlay must fail")
	}
}

func TestProvenance_RequireCompose(t *testing.T) {
	_, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: t.TempDir(), RequireCompose: true,
	})
	if !errors.Is(err, engine.ErrNoComposeFile) {
		t.Fatalf("want ErrNoComposeFile, got %v", err)
	}
}

func TestProvenance_RuntimeEnvironment(t *testing.T) {
	dir := t.TempDir()
	writeF(t, dir, "compose.yaml", `services:
  app:
    image: busybox
    env_file: [first.env, second.env]
    environment:
      INLINE: inline
      UNSET:
  other:
    image: busybox
    env_file: second.env
  raw:
    image: busybox
    env_file:
      - path: raw.env
        format: raw
`)
	writeF(t, dir, "first.env", "LOCAL=first\n")
	writeF(t, dir, "second.env", "COPY=${LOCAL:-missing}\nREF=${INLINE:-missing}\nUNSET=remove_me\n")
	writeF(t, dir, "raw.env", "RAW=${PROJECT}\nQUOTED=\"hello world\"\nHASH=value # retained\n")
	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir, Env: []string{"PROJECT=project"},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := findService(rep, "app")
	if entry(app, "COPY").Value != "first" || entry(app, "REF").Value != "inline" {
		t.Fatalf("sequential/inline lookup wrong: %+v", app)
	}
	for _, e := range app.Entries {
		if e.Key == "UNSET" {
			t.Fatalf("unresolved bare key must unset a file value: %+v", e)
		}
	}
	if entry(findService(rep, "other"), "COPY").Value != "missing" {
		t.Fatal("runtime values leaked between services")
	}
	raw := findService(rep, "raw")
	if entry(raw, "RAW").Value != "${PROJECT}" || entry(raw, "QUOTED").Value != `"hello world"` || entry(raw, "HASH").Value != "value # retained" {
		t.Fatalf("raw format was evaluated: %+v", raw)
	}
}

func TestProvenance_ProfileEffects(t *testing.T) {
	dir := t.TempDir()
	writeF(t, dir, "service.env", "RUNTIME=runtime\n")
	writeF(t, dir, "compose.yaml", `services:
  active: {image: busybox, env_file: service.env}
  inactive:
    image: busybox
    profiles: [extra]
    environment: {VALUE: "${RUNTIME:-fallback}"}
`)
	for _, enabled := range []bool{false, true} {
		profiles := []string{}
		if enabled {
			profiles = []string{"extra"}
		}
		rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{ProjectDir: dir, Profiles: profiles})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Vars["RUNTIME"].Gap != enabled {
			t.Fatalf("enabled=%v: RUNTIME=%+v", enabled, rep.Vars["RUNTIME"])
		}
	}
}

func TestProvenance_ConditionalEffects(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		env              []string
		gap              bool
	}{
		{"default skipped", "${SET:-${RUNTIME:-fallback}}", []string{"SET=chosen"}, false},
		{"default taken", "${SET:-${RUNTIME:-fallback}}", nil, true},
		{"alternative skipped", "${SET:+${RUNTIME:-fallback}}", nil, false},
		{"alternative taken", "${SET:+${RUNTIME:-fallback}}", []string{"SET=chosen"}, true},
		{"escaped", "$${RUNTIME}", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeF(t, dir, "service.env", "RUNTIME=runtime\n")
			writeF(t, dir, "compose.yaml", "services:\n  app:\n    image: busybox\n    env_file: service.env\n    environment:\n      VALUE: '"+tc.expression+"'\n")
			rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{ProjectDir: dir, Env: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			gr := provenance.CollectGaps(rep)
			if (gr.Count != 0) != tc.gap {
				t.Fatalf("unexpected conditional gap: %+v", gr)
			}
		})
	}
}
