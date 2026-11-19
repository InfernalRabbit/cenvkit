package provenance

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"tab kept", "a\tb", "a\tb"},
		{"ESC removed", "x\x1b[2Jy", "x[2Jy"},
		{"OSC removed", "x\x1b]0;t\x07y", "x]0;ty"},
		{"CR LF removed", "a\r\nb", "ab"},
		{"DEL removed", "a\x7fb", "ab"},
		{"C1 CSI removed", "a\u009bb", "ab"},
		{"utf8 kept", "порт=8080", "порт=8080"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Fatalf("%s: Sanitize(%q)=%q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestRenderHuman_ValueSanitized: a value carrying ANSI/OSC from an env file must
// never reach the human view as raw escape bytes (CWE-150).
func TestRenderHuman_ValueSanitized(t *testing.T) {
	rep := Report{Vars: map[string]VarTrace{
		"EVIL": {Value: "\x1b[2J\x1b]0;pwned\x07", InChain: true},
	}}
	var buf bytes.Buffer
	RenderHuman(&buf, rep, HumanOpts{Value: "EVIL"})
	out := buf.String()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("human output still carries ESC: %q", out)
	}
	if strings.ContainsRune(out, 0x07) {
		t.Fatalf("human output still carries BEL: %q", out)
	}
}
