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

// GetString returns the value for key. A double-quoted value is a QuotedString:
// the quotes are stripped and its escapes decoded.
func (m VarMap) GetString(key string) string {
	if v, ok := m[key]; ok {
		if (len(v) > 1) && (v[0] == '"') && (v[len(v)-1] == '"') {
			return unescape(v[1 : len(v)-1])
		}
		return v
	}
	return ""
}

// unescape decodes the C escapes Tor writes in a QuotedString: \n, \r and \t,
// three octal digits for a byte, and a backslash before any other byte for
// that byte itself, as in \\, \" and \'.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; {
		case c == 'n':
			b.WriteByte('\n')
		case c == 'r':
			b.WriteByte('\r')
		case c == 't':
			b.WriteByte('\t')
		case isOctalByte(s[i:]):
			b.WriteByte((s[i]-'0')<<6 | (s[i+1]-'0')<<3 | (s[i+2] - '0'))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// isOctalByte reports whether s opens with three octal digits naming one byte, 000 to 377.
func isOctalByte(s string) bool {
	if len(s) < 3 || s[0] < '0' || s[0] > '3' {
		return false
	}
	return s[1] >= '0' && s[1] <= '7' && s[2] >= '0' && s[2] <= '7'
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
