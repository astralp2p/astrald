package apphost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestHTTPServerPreflightNeedsNoToken(t *testing.T) {
	srv := &HTTPServer{Module: testTokenModule(t)}
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/user.info", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("OPTIONS status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestHTTPServerRefusesARequestWithoutAValidToken(t *testing.T) {
	mod := testTokenModule(t)
	expired, err := mod.CreateAccessToken(astral.GenerateIdentity(), astral.Duration(-time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	cases := []struct {
		name          string
		authorization string
	}{
		{"no Authorization", ""},
		{"expired token", "Bearer " + string(expired.Token)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := &HTTPServer{Module: mod}
			req := httptest.NewRequest(http.MethodGet, "/user.info", nil)
			if c.authorization != "" {
				req.Header.Set("Authorization", c.authorization)
			}
			rec := httptest.NewRecorder()

			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("GET status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestGetAuthToken(t *testing.T) {
	cases := []struct {
		authorization string
		want          string
	}{
		{"Bearer abc", "abc"},
		{"", ""},
		{"Basic xyz", ""},
	}

	srv := &HTTPServer{}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.authorization != "" {
			req.Header.Set("Authorization", c.authorization)
		}

		if got := srv.getAuthToken(req); got != c.want {
			t.Errorf("getAuthToken(%q) = %q, want %q", c.authorization, got, c.want)
		}
	}
}

// entryChan is a log.EntryLogger that forwards every entry to a channel.
type entryChan chan *log.Entry

func (c entryChan) LogEntry(e *log.Entry) { c <- e }

// TestHTTPRunWarnsOnNonLoopbackBind covers the bind warning: a wildcard bind
// logs a level-0 warning and a loopback bind does not.
func TestHTTPRunWarnsOnNonLoopbackBind(t *testing.T) {
	const warning = "is not loopback"

	cases := []struct {
		bind string
		warn bool
	}{
		{"tcp:0.0.0.0:0", true},
		{"tcp:127.0.0.1:0", false},
	}

	for _, c := range cases {
		t.Run(c.bind, func(t *testing.T) {
			// note: the channel is never closed; the shutdown goroutine logs after Run returns.
			entries := make(entryChan, 8)
			l := &log.Logger{}
			l.AddLogger(entries)

			ctx, cancel := astral.NewContext(nil).WithCancel()
			cancel()

			srv := &HTTPServer{Module: &Module{config: Config{BindHTTP: c.bind}, log: l}}
			if err := srv.Run(ctx); err != nil {
				t.Fatalf("run: %v", err)
			}

			var warned bool
			for n := len(entries); n > 0; n-- {
				e := <-entries
				var line string
				for _, obj := range e.Objects {
					line += astral.Stringify(obj)
				}
				if strings.Contains(line, warning) {
					warned = true
					if e.Level != 0 {
						t.Errorf("warning at level %v; want 0", e.Level)
					}
				}
			}
			if warned != c.warn {
				t.Errorf("warned = %v; want %v", warned, c.warn)
			}
		})
	}
}
