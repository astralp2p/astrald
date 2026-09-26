package core

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// entrySink records every entry the root logger emits.
//
// why no synchronization: LogEntry runs synchronously under the root mutex.
type entrySink struct{ entries []*log.Entry }

func (s *entrySink) LogEntry(e *log.Entry) {
	s.entries = append(s.entries, e)
}

// discard is the write end a test route hands back.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }

// recordingRoute records the query string each routed query carries, and
// answers it when accept is set.
type recordingRoute struct {
	accept   bool
	received []string
}

func (r *recordingRoute) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, _ io.WriteCloser) (io.WriteCloser, error) {
	r.received = append(r.received, q.QueryString.String())
	if !r.accept {
		return query.RouteNotFound()
	}
	return discard{}, nil
}

// blockAll is a preprocessor that blocks every query.
type blockAll struct{}

func (blockAll) PreprocessQuery(*QueryModifier) error { return errors.New("blocked by the test") }

// newTestRouter returns a node's router that logs every routing start into the returned sink.
func newTestRouter(t *testing.T, route *recordingRoute) (*Router, *entrySink) {
	t.Helper()

	logger := log.New(nil)
	// why: the filter gates the console writer alone; subscribers still receive
	// every entry, so this keeps the pump from printing during the test.
	logger.SetFilter(func(*log.Entry) bool { return false })
	sink := &entrySink{}
	logger.AddLogger(sink)

	node := &Node{
		identity: astral.GenerateIdentity(),
		config:   Config{LogRoutingStart: true},
		log:      logger,
	}
	node.Router = NewRouter(node)
	if err := node.Router.Add(route, 0); err != nil {
		t.Fatalf("add route: %v", err)
	}

	return node.Router, sink
}

// encodedEntries returns every entry as the log file stores it (binary) and as
// log.listen sends it in json.
func encodedEntries(t *testing.T, entries []*log.Entry) []byte {
	t.Helper()

	var buf bytes.Buffer
	for _, e := range entries {
		if _, err := e.WriteTo(&buf); err != nil {
			t.Fatalf("encode entry: %v", err)
		}
		j, err := e.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		buf.Write(j)
	}
	return buf.Bytes()
}

// TestRouteQueryLogsNoSecretArgument: every line the router logs about a query
// carries the query with each credential argument's value replaced, on every
// path (blocked, routed, failed), while the route still receives the query as
// sent. A node log held apphost.delete_token?token=<secret> for every revoked
// token, and apphost.register_handler's callback token, in the log file and in
// every log.listen stream.
func TestRouteQueryLogsNoSecretArgument(t *testing.T) {
	const secret = "ayqrDDVpewTCnnamdWILXWK5Yf3Ib1yM"

	cases := []struct {
		name      string
		query     string
		accept    bool
		block     bool
		wantLines int
		keep      string
	}{
		{"routed", "apphost.delete_token?token=" + secret, true, false, 2, "apphost.delete_token?token=<redacted>"},
		{"failed", "apphost.register_handler?endpoint=tcp:127.0.0.1:1&token=" + secret, false, false, 2, "endpoint=tcp:127.0.0.1:1&token=<redacted>"},
		{"blocked", "bip137sig.seed?passphrase=" + secret, true, true, 1, "bip137sig.seed?passphrase=<redacted>"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			route := &recordingRoute{accept: c.accept}
			router, sink := newTestRouter(t, route)
			if c.block {
				if err := router.AddQueryPreprocessor(blockAll{}); err != nil {
					t.Fatalf("add preprocessor: %v", err)
				}
			}

			q := astral.Launch(astral.NewQuery(nil, nil, c.query))
			_, _ = router.RouteQuery(astral.NewContext(nil), q, discard{})

			if len(sink.entries) != c.wantLines {
				t.Fatalf("logged %d entries, want %d", len(sink.entries), c.wantLines)
			}
			logged := encodedEntries(t, sink.entries)
			if bytes.Contains(logged, []byte(secret)) {
				t.Fatalf("a log entry carries the secret:\n%s", logged)
			}
			if !bytes.Contains(logged, []byte(c.keep)) {
				t.Fatalf("log entries lack %q:\n%s", c.keep, logged)
			}

			if q.QueryString.String() != c.query {
				t.Fatalf("query string after routing = %q, want %q", q.QueryString, c.query)
			}
			if !c.block && (len(route.received) != 1 || route.received[0] != c.query) {
				t.Fatalf("route received %q, want [%q]", route.received, c.query)
			}
		})
	}
}

// TestRedactQueryString: the value of every argument an op reads as token or
// passphrase is replaced, however its name is escaped, and the rest of the
// query string stays byte for byte.
func TestRedactQueryString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"apphost.delete_token?token=k7m2q5x9", "apphost.delete_token?token=<redacted>"},
		{"apphost.register_handler?endpoint=tcp:127.0.0.1:1&token=56aaffaa&out=json", "apphost.register_handler?endpoint=tcp:127.0.0.1:1&token=<redacted>&out=json"},
		{"bip137sig.seed?passphrase=two%20words&in=json", "bip137sig.seed?passphrase=<redacted>&in=json"},
		{"apphost.delete_token?tok%65n=k7m2q5x9", "apphost.delete_token?tok%65n=<redacted>"},
		{"op?token=a&token=b", "op?token=<redacted>&token=<redacted>"},
		{"op?token=", "op?token=<redacted>"},
		{"op?token", "op?token"},
		{"apphost.list_tokens?identity=02ab", "apphost.list_tokens?identity=02ab"},
		{"op?token_expires_at=1&Token=x", "op?token_expires_at=1&Token=x"},
		{"apphost.list_tokens", "apphost.list_tokens"},
		{"op?", "op?"},
	}

	for _, c := range cases {
		if got := redactQueryString(c.in); got != c.want {
			t.Errorf("redactQueryString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLoggedQueryLeavesTheQueryAlone: a query with nothing to hide is logged
// as itself, and a redacted copy never changes the query that is routed.
func TestLoggedQueryLeavesTheQueryAlone(t *testing.T) {
	plain := astral.NewQuery(nil, nil, "apphost.list_tokens?identity=02ab")
	if loggedQuery(plain) != plain {
		t.Fatalf("loggedQuery copied a query with no secret argument")
	}

	secret := astral.NewQuery(astral.GenerateIdentity(), astral.GenerateIdentity(), "apphost.delete_token?token=k7m2q5x9")
	logged := loggedQuery(secret)
	if secret.QueryString != "apphost.delete_token?token=k7m2q5x9" {
		t.Fatalf("loggedQuery changed the routed query to %q", secret.QueryString)
	}
	if logged.Nonce != secret.Nonce || logged.Caller != secret.Caller || logged.Target != secret.Target {
		t.Fatalf("logged copy names another query: %+v, want %+v", logged, secret)
	}
}
