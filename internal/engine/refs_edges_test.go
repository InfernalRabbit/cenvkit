package engine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/InfernalRabbit/cenvkit/internal/engine"
)

// TestProvenance_DepsEdges: WantDeps captures the interpolation edges of every
// raw layer entry, which is what --show deps will walk.
func TestProvenance_DepsEdges(t *testing.T) {
	dir := t.TempDir()
	env := writeF(t, dir, ".env", "B=1\nA=${B}\nC=${A:-x}\nPLAIN=no-refs\n")
	writeF(t, dir, "docker-compose.yml", "services:\n  web:\n    image: nginx\n")

	rep, err := engine.New().Provenance(context.Background(), engine.ProvInput{
		ProjectDir: dir,
		EnvFiles:   []engine.ProvFile{{Path: env, Layer: "layer1"}},
		WantLayers: true,
		WantDeps:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	type edge struct{ from, to string }
	got := map[edge]bool{}
	for _, r := range rep.Refs {
		got[edge{r.FromKey, r.ToVar}] = true
		if r.FromFile != env {
			t.Fatalf("edge from unexpected file: %+v", r)
		}
	}
	for _, want := range []edge{{"A", "B"}, {"C", "A"}} {
		if !got[want] {
			t.Fatalf("missing edge %v in %+v", want, rep.Refs)
		}
	}
	if len(rep.Refs) != 2 {
		t.Fatalf("want exactly 2 edges, got %d: %+v", len(rep.Refs), rep.Refs)
	}
	if _, err := filepath.Abs(env); err != nil {
		t.Fatal(err)
	}
}
