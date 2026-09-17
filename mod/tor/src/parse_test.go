package tor

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/tor"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/tor/tc"
)

const testOnionV3 = "pg6mmjiyjmcrsslvykfwnntlaru7p5svn6y2ymmju6nubxndf4pscryd"

func TestParseValidAddresses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPort int
		wantAddr string
	}{
		{"lowercase with port", testOnionV3 + ".onion:80", 80, testOnionV3 + ".onion:80"},
		{"uppercase with port", strings.ToUpper(testOnionV3) + ".ONION:80", 80, testOnionV3 + ".onion:80"},
		{"no port", testOnionV3, defaultListenPort, testOnionV3 + ".onion:1791"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.input, err)
			}
			if len(e.Digest) != tor.DigestSize {
				t.Errorf("digest is %d bytes; want %d", len(e.Digest), tor.DigestSize)
			}
			if int(e.Port) != tt.wantPort {
				t.Errorf("Port = %d; want %d", e.Port, tt.wantPort)
			}
			if got := e.Address(); got != tt.wantAddr {
				t.Errorf("Address() = %q; want %q", got, tt.wantAddr)
			}
		})
	}
}

func TestParseRejectsInvalidAddresses(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"non-numeric port", testOnionV3 + ":abc", "invalid address"},
		{"not base32", "abc.onion", "invalid address"},
		{"v2 length", "expyuzz4wqqyqhjn.onion", "invalid length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) = (%v, %v); want error containing %q", tt.input, e, err, tt.wantErr)
			}
		})
	}
}

func TestUnpackRoundTrip(t *testing.T) {
	e, err := Parse(testOnionV3 + ".onion:80")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := Unpack(e.Pack())
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if !bytes.Equal(got.Digest, e.Digest) || got.Port != e.Port {
		t.Errorf("Unpack = %v; want %v", got.Address(), e.Address())
	}

	if _, err := (&Module{}).Unpack("tcp", e.Pack()); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Module.Unpack(tcp) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}

	if _, err := Unpack(e.Pack()[:10]); err == nil {
		t.Error("Unpack(truncated) returned nil error; want an error")
	}
}

func TestListenerPrivateKey(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 64)

	tests := []struct {
		name       string
		privateKey string
		want       []byte
	}{
		{"ed25519 v3", "ED25519-V3:" + base64.StdEncoding.EncodeToString(key), key},
		{"rsa rejected", "RSA1024:abc", nil},
		{"bad base64", "ED25519-V3:!!!", nil},
		{"extra separator", "ED25519-V3:a:b", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := listener{onion: tc.Onion{PrivateKey: tt.privateKey}}
			if got := l.PrivateKey(); !bytes.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("PrivateKey() = %x; want %x", got, tt.want)
			}
		})
	}

	if got := (listener{onion: tc.Onion{ServiceID: "x"}}).Addr(); got != "x" {
		t.Errorf("Addr() = %q; want %q", got, "x")
	}
}

func TestConnEndpoints(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})

	e, err := Parse(testOnionV3)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	outbound := newConn(a, nil, true)
	if got := outbound.RemoteEndpoint(); got != nil {
		t.Errorf("RemoteEndpoint() with nil endpoint = %#v; want untyped nil", got)
	}
	if !outbound.Outbound() {
		t.Error("Outbound() = false; want true")
	}

	if got := newConn(a, &tor.Endpoint{}, false).RemoteEndpoint(); got != nil {
		t.Errorf("RemoteEndpoint() with zero endpoint = %#v; want untyped nil", got)
	}

	inbound := newConn(a, e, false)
	if got := inbound.RemoteEndpoint(); got != e {
		t.Errorf("RemoteEndpoint() = %v; want %v", got, e)
	}
	if inbound.Outbound() {
		t.Error("Outbound() = true; want false")
	}

	local, ok := inbound.LocalEndpoint().(*tor.Endpoint)
	if !ok || !local.IsZero() {
		t.Errorf("LocalEndpoint() = %#v; want a zero *tor.Endpoint", inbound.LocalEndpoint())
	}
}
