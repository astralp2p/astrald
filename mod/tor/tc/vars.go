package tc

import "strings"

// VarMap holds key=value pairs parsed from Tor control protocol response lines.
type VarMap map[string]string

// GetList returns the value for key split on commas, or an empty slice if the key is absent.
func (m VarMap) GetList(key string) []string {
	if v, ok := m[key]; ok {
		return strings.Split(v, ",")
	}
	return []string{}
}

// GetString returns the value for key, stripping surrounding double-quotes if present.
func (m VarMap) GetString(key string) string {
	if v, ok := m[key]; ok {
		if (len(v) > 1) && (v[0] == '"') && (v[len(v)-1] == '"') {
			return v[1 : len(v)-1]
		}
		return v
	}
	return ""
}

// splitWords splits a response line on spaces that sit outside a double-quoted string.
// note: a backslash inside quotes escapes the next byte; both bytes stay in the word.
func splitWords(line string) []string {
	var words []string
	var quoted, escaped bool
	start := 0
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ' ' && !quoted:
			words = append(words, line[start:i])
			start = i + 1
		}
	}
	return append(words, line[start:])
}

func parseVarMap(words []string) VarMap {
	m := make(map[string]string)
	for _, w := range words {
		kv := strings.SplitN(w, "=", 2)
		if len(kv) < 2 {
			continue
		}
		m[kv[0]] = kv[1]
	}
	return m
}
