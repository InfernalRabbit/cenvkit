package envmap

import (
	"strings"
	"testing"
)

// TestDotenvQuote_EscapesControls pins the escape set of dotenvQuote. compose-go
// decodes \r and \n back into the control characters (probe-verified on
// v2.17.0), so both must be escaped in the emitted line: a raw CR would split a
// line for line-oriented consumers or be normalized to LF by them.
func TestDotenvQuote_EscapesControls(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\rb", `"a\rb"`},
		{"a\nb", `"a\nb"`},
		{`a"b`, `"a\"b"`},
		{"a$b", `"a\$b"`},
		{`a\b`, `"a\\b"`},
	}
	for _, c := range cases {
		if got := dotenvQuote(c.in); got != c.want {
			t.Fatalf("dotenvQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The emitted line must never carry a raw CR or LF byte.
	if got := dotenvQuote("a\rb\nc"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("dotenvQuote emitted a raw control byte: %q", got)
	}
}
