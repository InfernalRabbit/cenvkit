package engine

// ParseReferences returns the variable names that a raw dotenv value depends on:
// ${VAR}, ${VAR:-def}, ${VAR-def}, ${VAR:?err}, ${VAR:+alt}, $(VAR) (braced) and $VAR.
// A "$$" is an escaped literal dollar and is not a reference. Nested references
// inside a default/alternate (e.g. ${A:-${B}}) are captured too, and the result is
// de-duplicated in first-seen order.
func ParseReferences(raw string) []string {
	out := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '$' {
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '$' { // $$ -> literal
			i++
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '{' {
			depth := 0
			end := len(raw)
			closed := false
			for j := i + 1; j < len(raw); j++ {
				if raw[j] == '{' {
					depth++
				}
				if raw[j] == '}' {
					depth--
					if depth == 0 {
						end = j
						closed = true
						break
					}
				}
			}
			if !closed {
				// Unterminated reference: treat the rest as literal rather than
				// inventing an edge the interpolation engine would not create.
				break
			}
			inner := ""
			if end > i+2 {
				inner = raw[i+2 : end]
			}
			add(leadingName(inner))
			for _, nested := range ParseReferences(inner) {
				add(nested)
			}
			i = end
			continue
		}
		j := i + 1
		for j < len(raw) && isNameByte(raw[j]) {
			j++
		}
		if j > i+1 {
			add(raw[i+1 : j])
		}
		i = j - 1
	}
	return out
}

// leadingName returns the variable-name prefix of a ${...} body: everything up to
// the first operator (:-, -, :?, :+, }) or end of string.
func leadingName(inner string) string {
	end := 0
	for end < len(inner) && isNameByte(inner[end]) {
		end++
	}
	if end == 0 {
		return ""
	}
	return inner[:end]
}

func isNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}
