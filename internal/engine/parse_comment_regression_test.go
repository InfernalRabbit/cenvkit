package engine_test

import (
	"testing"

	"github.com/InfernalRabbit/cenvkit/internal/engine"
)

// TestParseOrderedLiteral_QuotedWithTrailingComment is the regression guard for
// the --no-expand / --overview mismatch: a quoted value followed by an inline
// comment used to keep its quotes ("v" instead of v). compose-go cuts the
// comment first and unquotes second, so the literal reader must do the same.
func TestParseOrderedLiteral_QuotedWithTrailingComment(t *testing.T) {
	dir := t.TempDir()
	body := `A="v1" # trailing
B='v2' # c
C="v3 # not a comment"
`
	f := writeF(t, dir, "x.env", body)

	entries, err := engine.ParseOrderedLiteral(f)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"A": "v1",
		"B": "v2",
		"C": "v3 # not a comment", // "# " inside a quoted value is data, not a comment
	}
	if len(entries) != len(want) {
		t.Fatalf("want %d entries, got %d: %v", len(want), len(entries), entries)
	}
	for _, e := range entries {
		if e.RawValue != want[e.Key] {
			t.Fatalf("%s: RawValue=%q, want %q", e.Key, e.RawValue, want[e.Key])
		}
	}
}
