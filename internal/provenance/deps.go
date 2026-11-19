package provenance

import "sort"

// DepDefinition is one place a variable is defined.
type DepDefinition struct {
	File    string `json:"file"`
	Service string `json:"service,omitempty"`
	Layer   string `json:"layer"`
}

// DepEdge is one hop of a transitive dependency closure: Var reaches the closure
// at Depth from Root, through Via (the variable the walk came from). Key/File
// identify the referencing entry in the reverse direction.
type DepEdge struct {
	Var     string `json:"var"`
	Key     string `json:"key,omitempty"`
	File    string `json:"file,omitempty"`
	Service string `json:"service,omitempty"`
	Via     string `json:"via,omitempty"`
	Depth   int    `json:"depth"`
}

// DepConsumer is a service that receives the variable, either through a service
// env_file: definition or through a YAML ${VAR} reference it interpolates.
type DepConsumer struct {
	Service string `json:"service"`
	File    string `json:"file,omitempty"`
	Field   string `json:"field,omitempty"`
	Kind    string `json:"kind"` // env_file | interpolation
}

// DepReport is the dependency closure of one variable: where it is defined, what
// it needs (forward), who needs it (reverse) and which services consume it.
type DepReport struct {
	Root        string          `json:"root"`
	Definitions []DepDefinition `json:"definitions"`
	DependsOn   []DepEdge       `json:"depends_on,omitempty"`
	NeededBy    []DepEdge       `json:"needed_by,omitempty"`
	Consumers   []DepConsumer   `json:"consumers,omitempty"`
}

// BuildDeps computes the closure for name from the report's raw layers, refs,
// vars and services. Definitions are matched per file (the granularity the layers
// carry), so a key defined in several files yields several definitions.
func BuildDeps(r Report, name string) DepReport {
	out := DepReport{Root: name}
	defsByVar := map[string][]DepDefinition{}
	for _, l := range r.Layers {
		for _, e := range l.Entries {
			d := DepDefinition{File: l.File, Service: l.Service, Layer: l.Layer}
			defsByVar[e.Key] = append(defsByVar[e.Key], d)
			if e.Key == name {
				out.Definitions = append(out.Definitions, d)
			}
		}
	}
	if len(out.Definitions) == 0 {
		if vt, ok := r.Vars[name]; ok {
			if vt.Winner.File != "" {
				d := DepDefinition{File: vt.Winner.File, Layer: vt.Winner.Layer}
				out.Definitions = append(out.Definitions, d)
				defsByVar[name] = append(defsByVar[name], d)
			}
			for _, rd := range vt.RuntimeDefs {
				d := DepDefinition{File: rd.File, Service: rd.Service, Layer: "env_file"}
				out.Definitions = append(out.Definitions, d)
				defsByVar[name] = append(defsByVar[name], d)
			}
		}
	}
	sort.Slice(out.Definitions, func(i, j int) bool {
		a, b := out.Definitions[i], out.Definitions[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.Layer < b.Layer
	})

	fromIndex := map[[2]string][]Ref{}
	toIndex := map[string][]Ref{}
	for _, ref := range r.Refs {
		k := [2]string{ref.FromFile, ref.FromKey}
		fromIndex[k] = append(fromIndex[k], ref)
		toIndex[ref.ToVar] = append(toIndex[ref.ToVar], ref)
	}

	// Forward closure: what Root needs, transitively.
	type hop struct {
		name  string
		depth int
	}
	seenF := map[string]bool{name: true}
	queue := []hop{{name, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, def := range defsByVar[cur.name] {
			for _, ref := range fromIndex[[2]string{def.File, cur.name}] {
				if seenF[ref.ToVar] {
					continue
				}
				seenF[ref.ToVar] = true
				out.DependsOn = append(out.DependsOn, DepEdge{
					Var: ref.ToVar, File: def.File, Service: def.Service, Via: cur.name, Depth: cur.depth + 1,
				})
				queue = append(queue, hop{ref.ToVar, cur.depth + 1})
			}
		}
	}

	// Reverse closure: who references Root, transitively.
	seenR := map[string]bool{name: true}
	queue = []hop{{name, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, ref := range toIndex[cur.name] {
			if seenR[ref.FromKey] {
				continue
			}
			seenR[ref.FromKey] = true
			out.NeededBy = append(out.NeededBy, DepEdge{
				Var: ref.FromKey, Key: ref.FromKey, File: ref.FromFile,
				Service: ref.FromService, Via: cur.name, Depth: cur.depth + 1,
			})
			queue = append(queue, hop{ref.FromKey, cur.depth + 1})
		}
	}

	// Consumers: services that receive Root.
	for _, l := range r.Layers {
		if l.Layer != "env_file" {
			continue
		}
		for _, e := range l.Entries {
			if e.Key == name {
				out.Consumers = append(out.Consumers, DepConsumer{Service: l.Service, File: l.File, Kind: "env_file"})
			}
		}
	}
	if vt, ok := r.Vars[name]; ok {
		for _, e := range vt.Effects {
			out.Consumers = append(out.Consumers, DepConsumer{Service: e.Service, Field: e.Field, Kind: "interpolation"})
		}
	}

	sort.Slice(out.DependsOn, func(i, j int) bool {
		a, b := out.DependsOn[i], out.DependsOn[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Var < b.Var
	})
	sort.Slice(out.NeededBy, func(i, j int) bool {
		a, b := out.NeededBy[i], out.NeededBy[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Key < b.Key
	})
	sort.Slice(out.Consumers, func(i, j int) bool {
		a, b := out.Consumers[i], out.Consumers[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Field < b.Field
	})
	return out
}
