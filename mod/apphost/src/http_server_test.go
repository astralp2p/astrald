package apphost

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
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
