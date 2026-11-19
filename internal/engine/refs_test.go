package engine_test

import (
	"reflect"
	"testing"

	"github.com/InfernalRabbit/cenvkit/internal/engine"
)

func TestParseReferences(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"plain", "value", nil},
		{"simple", "${A}", []string{"A"}},
		{"default", "${A:-fallback}", []string{"A"}},
		{"nested default", "${A:-${B}}", []string{"A", "B"}},
		{"dash default", "${A-x}", []string{"A"}},
		{"plus alt", "${A:+on}", []string{"A"}},
		{"question", "${A:?missing}", []string{"A"}},
		{"bare dollar name", "$A/suffix", []string{"A"}},
		{"escaped dollar", "cost: $$5 and $$A", nil},
		{"two refs", "${A}-${B}", []string{"A", "B"}},
		{"dup", "${A}${A}", []string{"A"}},
		{"unterminated", "${A", nil},
		{"underscore", "${_A1}", []string{"_A1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := engine.ParseReferences(c.raw)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ParseReferences(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}
