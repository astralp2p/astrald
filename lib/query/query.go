package query

import (
	"net/url"
	"strings"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
)

// DefaultArgKey is the map key used for positional (non-flagged) arguments.
const DefaultArgKey = "arg"
const maxQueryTimeout = 60 * time.Second

type Validator interface {
	Validate() error
}

type Args map[string]any

// New returns a new instance of astral.InFlightQuery. Args can be:
// - a string - http-formatted arguments like "a=1&b=2"
// - a map[string]any - as long as all values are convertible to a string (string, Stringer, TextMarshaler)
func New(caller *astral.Identity, target *astral.Identity, path string, args any) (query *astral.Query) {
	query = &astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      caller,
		Target:      target,
		QueryString: path,
	}

	if args == nil {
		return
	}

	str, err := Marshal(args)
	if err != nil {
		return
	}

	if len(str) > 0 {
		query.QueryString += "?" + str
	}

	return
}

// Parse parses a query string (like "method?a=1&a=2") into a path and params
func Parse(q string) (path string, params map[string]string) {
	var s string
	path, s = splitPathParams(q)
	params = map[string]string{}

	vals, err := url.ParseQuery(s)
	if err != nil {
		return
	}

	for k, v := range vals {
		if len(v) > 0 {
			params[k] = v[0]
		} else {
			params[DefaultArgKey] = k
		}
	}

	return
}

func splitPathParams(query string) (path, params string) {
	if i := strings.IndexByte(query, '?'); i != -1 {
		return query[:i], query[i+1:]
	}
	return query, ""
}
