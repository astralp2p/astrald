package tc

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeControlPort serves one control connection on a net.Pipe. It answers PROTOCOLINFO with
// methods and cookieFile, answers AUTHCHALLENGE with hashes made from cookie (nil: random bytes),
// and answers every other command with 250 OK. It returns the client end and every line received.
func fakeControlPort(t *testing.T, methods, cookieFile string, cookie []byte) (net.Conn, <-chan []string) {
	t.Helper()
	client, server := net.Pipe()
	received := make(chan []string, 1)

	go func() {
		defer server.Close()
		var lines []string
		defer func() { received <- lines }()

		r := bufio.NewReader(server)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			lines = append(lines, line)

			if _, err = server.Write([]byte(fakeReply(line, methods, cookieFile, cookie))); err != nil {
				return
			}
		}
	}()

	return client, received
}

func fakeReply(line, methods, cookieFile string, cookie []byte) string {
	switch {
	case strings.HasPrefix(line, "PROTOCOLINFO"):
		return "250-PROTOCOLINFO 1\r\n" +
			fmt.Sprintf("250-AUTH METHODS=%s COOKIEFILE=%q\r\n", methods, cookieFile) +
			"250-VERSION Tor=\"0.4.9.14\"\r\n250 OK\r\n"

	case strings.HasPrefix(line, "AUTHCHALLENGE SAFECOOKIE "):
		clientNonce, _ := hex.DecodeString(strings.TrimPrefix(line, "AUTHCHALLENGE SAFECOOKIE "))
		serverNonce := randomBytes(cookieLen)
		serverHash := randomBytes(32)
		if cookie != nil {
			serverHash = safeCookieHash(serverHashKey, cookie, clientNonce, serverNonce)
		}
		return fmt.Sprintf("250 AUTHCHALLENGE SERVERHASH=%X SERVERNONCE=%X\r\n", serverHash, serverNonce)
	}

	return "250 OK\r\n"
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// writeSecret writes a file of n known bytes outside any Tor directory.
func writeSecret(t *testing.T, n int) (string, []byte) {
	t.Helper()
	secret := randomBytes(n)
	path := filepath.Join(t.TempDir(), "node_key")
	if err := os.WriteFile(path, secret, 0600); err != nil {
		t.Fatal(err)
	}
	return path, secret
}

// assertNotSent fails if any received line carries secret, raw or hex in either case.
func assertNotSent(t *testing.T, lines []string, secret []byte) {
	t.Helper()
	all := strings.Join(lines, "\n")
	for _, form := range []string{string(secret), hex.EncodeToString(secret), strings.ToUpper(hex.EncodeToString(secret))} {
		if strings.Contains(all, form) {
			t.Fatalf("the control port received the file's bytes; lines: %q", lines)
		}
	}
}

// TestAuthenticateNeverSendsANamedFile: a control port that names an arbitrary file as COOKIEFILE
// receives none of its bytes, whatever methods it offers and whatever the file's size.
func TestAuthenticateNeverSendsANamedFile(t *testing.T) {
	for _, c := range []struct {
		name    string
		methods string
		size    int
	}{
		{"COOKIE only, key-sized file", "COOKIE", cookieLen},
		{"COOKIE and SAFECOOKIE, key-sized file", "COOKIE,SAFECOOKIE", cookieLen},
		{"COOKIE and SAFECOOKIE, other size", "COOKIE,SAFECOOKIE", 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			path, secret := writeSecret(t, c.size)
			conn, received := fakeControlPort(t, c.methods, path, nil)

			err := New(conn).Authenticate()
			conn.Close()
			lines := <-received

			if err == nil {
				t.Fatal("Authenticate succeeded against a control port that does not know the cookie")
			}
			assertNotSent(t, lines, secret)
			for _, line := range lines {
				if strings.HasPrefix(line, "AUTHENTICATE") {
					t.Fatalf("AUTHENTICATE sent to a control port that proved nothing: %q", line)
				}
			}
		})
	}
}

// TestAuthenticateWithSafeCookie: a control port that knows the cookie gets the client hash, and
// the cookie itself is never sent.
func TestAuthenticateWithSafeCookie(t *testing.T) {
	path, cookie := writeSecret(t, cookieLen)
	conn, received := fakeControlPort(t, "COOKIE,SAFECOOKIE", path, cookie)

	err := New(conn).Authenticate()
	conn.Close()
	lines := <-received

	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	assertNotSent(t, lines, cookie)

	var auth string
	for _, line := range lines {
		if strings.HasPrefix(line, "AUTHENTICATE ") {
			auth = line
		}
	}
	if len(auth) != len("AUTHENTICATE ")+64 {
		t.Fatalf("AUTHENTICATE line = %q, want a 32-byte hex client hash", auth)
	}
}

func TestReadCookieRefusesNonCookieFiles(t *testing.T) {
	dir := t.TempDir()
	short := filepath.Join(dir, "short")
	os.WriteFile(short, bytes.Repeat([]byte{1}, cookieLen-1), 0600)

	for _, path := range []string{short, dir, filepath.Join(dir, "missing")} {
		if _, err := readCookie(path); err == nil {
			t.Errorf("readCookie(%q) succeeded, want an error", path)
		}
	}
}
