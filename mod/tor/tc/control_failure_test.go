package tc

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// fakeConn drives a Control against a scripted reply and an optionally failing
// write, with no socket and no daemon.
type fakeConn struct {
	io.Reader
	writeErr error
	written  bytes.Buffer
}

func (c *fakeConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return c.written.Write(p)
}

func (c *fakeConn) Close() error { return nil }

// TestAuthenticateWithoutProtocolInfo: a daemon that refuses PROTOCOLINFO, or
// hangs up, leaves ProtocolInfo nil and Authenticate must report that rather
// than dereference it -- the panic runs inside Server.Run and kills the node.
func TestAuthenticateWithoutProtocolInfo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
	}{
		{"error reply", "510 Unrecognized command\r\n"},
		{"eof", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &fakeConn{Reader: strings.NewReader(tc.reply)}

			if err := New(conn).Authenticate(); err == nil {
				t.Fatal("Authenticate returned nil for a daemon with no protocol info")
			}
		})
	}
}

// TestRequestPropagatesWriteError: a command that never reached the daemon is
// not a success. Before the fix AddOnion returned an empty Onion with a nil
// error and DelOnion reported a removal that never happened.
func TestRequestPropagatesWriteError(t *testing.T) {
	wantErr := errors.New("transport is down")

	t.Run("AddOnion", func(t *testing.T) {
		// why one Control per request: a Control that has failed a write is not
		// reusable, so each assertion gets its own.
		conn := &fakeConn{Reader: strings.NewReader(""), writeErr: wantErr}

		onion, err := New(conn).AddOnion(KeyNewV3, nil)
		if !errors.Is(err, wantErr) {
			t.Fatalf("AddOnion err = %v; want %v", err, wantErr)
		}
		if onion.ServiceID != "" || onion.PrivateKey != "" {
			t.Errorf("AddOnion returned %+v; want the zero Onion", onion)
		}
	})

	t.Run("DelOnion", func(t *testing.T) {
		conn := &fakeConn{Reader: strings.NewReader(""), writeErr: wantErr}

		if err := New(conn).DelOnion("abcdef"); !errors.Is(err, wantErr) {
			t.Fatalf("DelOnion err = %v; want %v", err, wantErr)
		}
	})
}

// TestParseVarMapSkipsWordWithoutEquals: a bare word is skipped, not indexed.
func TestParseVarMapSkipsWordWithoutEquals(t *testing.T) {
	m := parseVarMap([]string{"METHODS=COOKIE", "bare"})

	if len(m) != 1 {
		t.Fatalf("parseVarMap returned %v; want only the key=value word", m)
	}
	if got := m["METHODS"]; got != "COOKIE" {
		t.Errorf("METHODS = %q; want COOKIE", got)
	}
}

// TestParseProtocolInfoCookieFileWithSpace: the control spec allows a quoted
// COOKIEFILE containing a space, which the single-space split turns into a bare
// word. That must not panic.
//
// note: the cookie path is deliberately not asserted. The guard stops the crash
// but leaves the path truncated at the space, because parseProtocolInfo does not
// tokenize QuotedStrings; fixing that is a separate change and must not have to
// rewrite this test.
func TestParseProtocolInfoCookieFileWithSpace(t *testing.T) {
	info, err := parseProtocolInfo([]string{
		`AUTH METHODS=COOKIE COOKIEFILE="/var/run/my tor/control.authcookie"`,
	})
	if err != nil {
		t.Fatalf("parseProtocolInfo: %v", err)
	}

	if !slices.Equal(info.AuthMethods, []string{"COOKIE"}) {
		t.Errorf("AuthMethods = %v; want [COOKIE]", info.AuthMethods)
	}
}

// TestSecondReadErrorDoesNotPanic: watch.Read closes closeCh exactly once, so a
// second failing request on the same Control reports an error instead of
// panicking with "close of closed channel".
func TestSecondReadErrorDoesNotPanic(t *testing.T) {
	conn := &fakeConn{Reader: strings.NewReader("")}
	ctl := New(conn)

	if _, err := ctl.GetInfo("version"); err == nil {
		t.Fatal("first GetInfo returned nil over a dead transport")
	}
	if _, err := ctl.GetInfo("version"); err == nil {
		t.Fatal("second GetInfo returned nil over a dead transport")
	}

	select {
	case <-ctl.WaitClose():
	default:
		t.Error("WaitClose is still open after the transport was detected as closed")
	}
}
