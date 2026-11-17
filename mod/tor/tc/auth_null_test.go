package tc

import (
	"slices"
	"strings"
	"testing"
)

// TestAuthenticateOverUnixSocketAcceptsNull: a daemon that asks for no authentication, as
// tor-android's control socket does, is answered with a bare AUTHENTICATE.
func TestAuthenticateOverUnixSocketAcceptsNull(t *testing.T) {
	conn, received := fakeControlPort(t, "NULL", "", nil)

	if err := New(conn).AuthenticateOverUnixSocket(); err != nil {
		t.Fatalf("AuthenticateOverUnixSocket: %v", err)
	}
	conn.Close()

	if lines := <-received; !slices.Contains(lines, "AUTHENTICATE") {
		t.Fatalf("no bare AUTHENTICATE was sent; lines: %q", lines)
	}
}

// TestAuthenticateRefusesNull: over TCP any local process reaches the control port, so a daemon
// that asks for no authentication is refused.
func TestAuthenticateRefusesNull(t *testing.T) {
	conn, received := fakeControlPort(t, "NULL", "", nil)

	if err := New(conn).Authenticate(); err == nil {
		t.Fatal("Authenticate accepted a control port that asks for no authentication")
	}
	conn.Close()

	for _, line := range <-received {
		if strings.HasPrefix(line, "AUTHENTICATE") {
			t.Fatalf("Authenticate sent %q to a NULL-only control port", line)
		}
	}
}

// TestAuthenticateOverUnixSocketPrefersSafeCookie: a daemon that offers both methods gets the
// SAFECOOKIE exchange, not the empty one.
func TestAuthenticateOverUnixSocketPrefersSafeCookie(t *testing.T) {
	path, cookie := writeSecret(t, cookieLen)
	conn, received := fakeControlPort(t, "NULL,SAFECOOKIE", path, cookie)

	if err := New(conn).AuthenticateOverUnixSocket(); err != nil {
		t.Fatalf("AuthenticateOverUnixSocket: %v", err)
	}
	conn.Close()

	lines := <-received
	if slices.Contains(lines, "AUTHENTICATE") {
		t.Fatalf("a bare AUTHENTICATE was sent although SAFECOOKIE was offered; lines: %q", lines)
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "AUTHCHALLENGE SAFECOOKIE ") }) {
		t.Fatalf("no SAFECOOKIE challenge; lines: %q", lines)
	}
}
