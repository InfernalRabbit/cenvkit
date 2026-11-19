package provenance

import "testing"

// depsReport is a small hand-built model: A depends on B (transitively D), C and
// the web env_file key reference A, and service api interpolates A in YAML.
func depsReport() Report {
	return Report{
		Vars: map[string]VarTrace{
			"A": {Name: "A", InChain: true, Effects: []Effect{{Service: "api", Field: "environment.A"}}},
		},
		Layers: []OverviewLayer{
			{File: "/p/.env", Layer: "layer1", Entries: []OverviewEntry{
				{Key: "B", RawValue: "1"},
				{Key: "A", RawValue: "${B}"},
				{Key: "C", RawValue: "${A}"},
				{Key: "D", RawValue: "${B}"},
			}},
			{File: "/p/web/.web.env", Layer: "env_file", Service: "web", Entries: []OverviewEntry{
				{Key: "WEB", RawValue: "${A}"},
			}},
		},
		Refs: []Ref{
			{FromFile: "/p/.env", FromKey: "A", FromLayer: "layer1", ToVar: "B"},
			{FromFile: "/p/.env", FromKey: "C", FromLayer: "layer1", ToVar: "A"},
			{FromFile: "/p/.env", FromKey: "D", FromLayer: "layer1", ToVar: "B"},
			{FromFile: "/p/web/.web.env", FromKey: "WEB", FromService: "web", FromLayer: "env_file", ToVar: "A"},
		},
	}
}

func TestBuildDeps(t *testing.T) {
	d := BuildDeps(depsReport(), "A")
	if len(d.Definitions) != 1 || d.Definitions[0].File != "/p/.env" || d.Definitions[0].Layer != "layer1" {
		t.Fatalf("definitions: %+v", d.Definitions)
	}
	if len(d.DependsOn) != 1 || d.DependsOn[0].Var != "B" || d.DependsOn[0].Depth != 1 {
		t.Fatalf("depends_on: %+v", d.DependsOn)
	}
	// Reverse closure is transitive: C -> A, and nothing references C.
	if len(d.NeededBy) != 2 {
		t.Fatalf("needed_by: %+v", d.NeededBy)
	}
	seen := map[string]bool{}
	for _, e := range d.NeededBy {
		seen[e.Key] = true
	}
	if !seen["C"] || !seen["WEB"] {
		t.Fatalf("needed_by must contain C and WEB: %+v", d.NeededBy)
	}
	if len(d.Consumers) != 1 || d.Consumers[0].Service != "api" || d.Consumers[0].Kind != "interpolation" {
		t.Fatalf("consumers: %+v", d.Consumers)
	}
}

func TestBuildDeps_TransitiveForward(t *testing.T) {
	rep := depsReport()
	// B references D2, so A -> B -> D2.
	rep.Layers[0].Entries = append(rep.Layers[0].Entries, OverviewEntry{Key: "B", RawValue: "${D2}"})
	rep.Refs = append(rep.Refs, Ref{FromFile: "/p/.env", FromKey: "B", FromLayer: "layer1", ToVar: "D2"})
	rep.Layers[0].Entries = append(rep.Layers[0].Entries, OverviewEntry{Key: "D2", RawValue: "2"})
	d := BuildDeps(rep, "A")
	byVar := map[string]int{}
	for _, e := range d.DependsOn {
		byVar[e.Var] = e.Depth
	}
	if byVar["B"] != 1 || byVar["D2"] != 2 {
		t.Fatalf("forward closure: %+v", d.DependsOn)
	}
}
