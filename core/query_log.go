package core

import (
	"net/url"
	"strings"

	"github.com/astralp2p/astral-go/astral"
)

// secretArgs names the query arguments whose value is a credential.
// note: apphost.delete_token and apphost.register_handler take a token, and bip137sig.seed takes a passphrase.
var secretArgs = map[string]bool{"token": true, "passphrase": true}

// redactedValue stands in a logged query for the value of a secret argument.
const redactedValue = "<redacted>"

// loggedQuery returns the query as the router logs it: q itself, or a copy whose secret argument values are replaced.
// why: a log entry keeps the query object, and the log file and every log.listen subscriber receive it whole.
func loggedQuery(q *astral.Query) *astral.Query {
	s := redactQueryString(q.QueryString.String())
	if s == q.QueryString.String() {
		return q
	}

	c := *q
	c.QueryString = astral.String32(s)
	return &c
}

// redactQueryString replaces the value of every secret argument and keeps the rest of s byte for byte.
func redactQueryString(s string) string {
	path, params, found := strings.Cut(s, "?")
	if !found {
		return s
	}

	pairs := strings.Split(params, "&")
	for i, pair := range pairs {
		key, _, hasValue := strings.Cut(pair, "=")
		if hasValue && secretArgs[argName(key)] {
			pairs[i] = key + "=" + redactedValue
		}
	}

	return path + "?" + strings.Join(pairs, "&")
}

// argName returns the argument name an op reads from an encoded key.
func argName(key string) string {
	name, err := url.QueryUnescape(key)
	if err != nil {
		return key
	}
	return name
}
