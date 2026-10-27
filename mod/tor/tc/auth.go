package tc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// cookieLen is the length of a Tor control auth cookie.
const cookieLen = 32

// SAFECOOKIE HMAC keys, from the Tor control protocol (control-spec.txt, AUTHCHALLENGE).
const (
	serverHashKey = "Tor safe cookie authentication server-to-controller hash"
	clientHashKey = "Tor safe cookie authentication controller-to-server hash"
)

var errServerHash = errors.New("tor control: server hash does not prove the cookie")

// Authenticate selects an auth method supported by both the daemon and this client and performs it.
// Returns an error if the daemon's protocol info is unavailable, or if no mutually supported
// method is available.
//
// why SAFECOOKIE only: COOKIE sends the bytes of the file that the control port names, so any
// process listening on the control address reads any file the node can read. With SAFECOOKIE the
// daemon proves it knows the cookie before the client answers, and the answer is an HMAC.
func (ctl *Control) Authenticate() error {
	info := ctl.ProtocolInfo()
	if info == nil {
		return errors.New("protocol info unavailable")
	}

	if info.HasAuthMethod(authMethodSafeCookie) {
		return ctl.authenticateWithSafeCookie(info)
	}

	return errors.New("no supported auth method")
}

func (ctl *Control) authenticateWithSafeCookie(info *ProtocolInfo) error {
	cookie, err := readCookie(info.AuthCookieFile)
	if err != nil {
		return err
	}

	clientNonce := make([]byte, cookieLen)
	if _, err = rand.Read(clientNonce); err != nil {
		return err
	}

	_, lines, err := ctl.request("AUTHCHALLENGE SAFECOOKIE %s", hex.EncodeToString(clientNonce))
	if err != nil {
		return err
	}

	serverHash, serverNonce, err := parseAuthChallenge(lines)
	if err != nil {
		return err
	}

	if !hmac.Equal(serverHash, safeCookieHash(serverHashKey, cookie, clientNonce, serverNonce)) {
		return errServerHash
	}

	clientHash := safeCookieHash(clientHashKey, cookie, clientNonce, serverNonce)
	_, _, err = ctl.request("AUTHENTICATE %s", hex.EncodeToString(clientHash))

	return err
}

// readCookie reads the cookie file at path. The file must be a regular file of cookieLen bytes.
//
// why: the path comes from the control port, so a file that cannot be a cookie is never read.
func readCookie(path string) ([]byte, error) {
	if err := checkCookieFile(os.Stat(path)); err != nil {
		return nil, fmt.Errorf("cookie file %q: %w", path, err)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// note: the second check covers a file replaced between the stat and the open
	if err = checkCookieFile(f.Stat()); err != nil {
		return nil, fmt.Errorf("cookie file %q: %w", path, err)
	}

	cookie := make([]byte, cookieLen)
	if _, err = io.ReadFull(f, cookie); err != nil {
		return nil, fmt.Errorf("cookie file %q: %w", path, err)
	}

	return cookie, nil
}

func checkCookieFile(fi os.FileInfo, err error) error {
	switch {
	case err != nil:
		return err
	case !fi.Mode().IsRegular():
		return errors.New("not a regular file")
	case fi.Size() != cookieLen:
		return fmt.Errorf("size %v, want %v", fi.Size(), cookieLen)
	}
	return nil
}

// parseAuthChallenge reads SERVERHASH and SERVERNONCE from an AUTHCHALLENGE reply.
func parseAuthChallenge(lines []string) (serverHash, serverNonce []byte, err error) {
	if len(lines) == 0 {
		return nil, nil, errors.New("empty AUTHCHALLENGE reply")
	}

	words := splitWords(lines[0])
	if words[0] != "AUTHCHALLENGE" {
		return nil, nil, fmt.Errorf("unexpected AUTHCHALLENGE reply %q", lines[0])
	}
	vars := parseVarMap(words[1:])

	if serverHash, err = hex.DecodeString(vars.GetString("SERVERHASH")); err != nil {
		return nil, nil, fmt.Errorf("SERVERHASH: %w", err)
	}
	if serverNonce, err = hex.DecodeString(vars.GetString("SERVERNONCE")); err != nil {
		return nil, nil, fmt.Errorf("SERVERNONCE: %w", err)
	}

	return serverHash, serverNonce, nil
}

// safeCookieHash is HMAC-SHA256 under key of cookie, clientNonce and serverNonce.
func safeCookieHash(key string, cookie, clientNonce, serverNonce []byte) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(cookie)
	mac.Write(clientNonce)
	mac.Write(serverNonce)
	return mac.Sum(nil)
}
