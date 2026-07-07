package objects

import (
	"encoding"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &SearchQuery{}
var _ encoding.TextMarshaler = SearchQuery{}
var _ encoding.TextUnmarshaler = &SearchQuery{}

type SearchQuery struct {
	Query astral.String16
	Tags  []QueryTag
}

func (SearchQuery) ObjectType() string { return "objects.search_query" }

func (q SearchQuery) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&q).WriteTo(w)
}

func (q *SearchQuery) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(q).ReadFrom(r)
}

// RequiredTagsIn returns true if all required (tag:value) and exclude (-tag:value) tags
// in the query are present in knownTags. Optional tags are ignored.
func (q *SearchQuery) RequiredTagsIn(knownTags ...string) error {
	known := make(map[string]struct{}, len(knownTags))
	for _, t := range knownTags {
		known[t] = struct{}{}
	}
	for _, tag := range q.Tags {
		switch tag.Mod {
		case TagModRequire, TagModExclude:
			if _, ok := known[string(tag.Name)]; !ok {
				return NewErrTagUnsupported(string(tag.Name))
			}
		}
	}
	return nil
}

// MarshalText encodes the SearchQuery back to its raw string representation.
func (q SearchQuery) MarshalText() ([]byte, error) {
	var tokens []string

	for _, tag := range q.Tags {
		var prefix string
		switch tag.Mod {
		case TagModExclude:
			prefix = "-"
		case TagModOptional:
			prefix = "?"
		case TagModOptionalExclude:
			prefix = "~"
		}
		value := string(tag.Value)
		if strings.ContainsRune(value, ' ') {
			value = `"` + value + `"`
		}
		tokens = append(tokens, prefix+string(tag.Name)+":"+value)
	}

	if s := strings.TrimSpace(string(q.Query)); s != "" {
		if strings.ContainsRune(s, ' ') {
			s = `"` + s + `"`
		}
		tokens = append(tokens, s)
	}

	return []byte(strings.Join(tokens, " ")), nil
}

// UnmarshalText parses a raw query string into a SearchQuery.
// Grammar: bare words accumulate into Query; tag:value is required;
// -tag:value is excluded; ?tag:value is optional.
// Values (and bare words) may be double-quoted to include spaces: title:"around the world"
func (q *SearchQuery) UnmarshalText(text []byte) error {
	var words []string
	q.Tags = nil

	for _, token := range tokenizeQuery(string(text)) {
		var mod QueryTagMod
		switch {
		case strings.HasPrefix(token, "-"):
			mod = TagModExclude
			token = token[1:]
		case strings.HasPrefix(token, "?"):
			mod = TagModOptional
			token = token[1:]
		case strings.HasPrefix(token, "~"):
			mod = TagModOptionalExclude
			token = token[1:]
		}

		if name, value, ok := strings.Cut(token, ":"); ok {
			q.Tags = append(q.Tags, QueryTag{
				Name:  astral.String8(strings.ToLower(name)),
				Mod:   mod,
				Value: astral.String8(strings.ToLower(value)),
			})
		} else {
			words = append(words, strings.ToLower(token))
		}
	}

	q.Query = astral.String16(strings.Join(words, " "))
	return nil
}

func (q SearchQuery) String() string {
	b, _ := q.MarshalText()
	return string(b)
}

// tokenizeQuery splits s by whitespace, respecting double-quoted strings.
func tokenizeQuery(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false

	for _, ch := range s {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func init() {
	astral.Add(&SearchQuery{})
}
