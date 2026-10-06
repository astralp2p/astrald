package tc

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// why: each Control gets its own scripted reply, so no request reads past EOF.
type scriptedConn struct {
	*strings.Reader
	written bytes.Buffer
}

func newScriptedConn(reply string) *scriptedConn {
	return &scriptedConn{Reader: strings.NewReader(reply)}
}

func (c *scriptedConn) Write(p []byte) (int, error) { return c.written.Write(p) }
func (c *scriptedConn) Close() error                { return nil }

func TestVarMapGetString(t *testing.T) {
	tests := []struct {
		name string
		m    VarMap
		want string
	}{
		{"quoted", VarMap{"k": `"v"`}, "v"},
		{"bare", VarMap{"k": "v"}, "v"},
		{"lone quote kept", VarMap{"k": `"`}, `"`},
		{"missing key", VarMap{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.GetString("k"); got != tt.want {
				t.Errorf("GetString(%q) = %q; want %q", "k", got, tt.want)
			}
		})
	}
}

func TestVarMapGetList(t *testing.T) {
	got := VarMap{"M": "COOKIE,SAFECOOKIE"}.GetList("M")
	if want := []string{"COOKIE", "SAFECOOKIE"}; !slices.Equal(got, want) {
		t.Errorf("GetList(M) = %q; want %q", got, want)
	}

	missing := VarMap{}.GetList("M")
	if missing == nil || len(missing) != 0 {
		t.Errorf("GetList(missing) = %#v; want empty non-nil slice", missing)
	}
}

func TestParseProtocolInfo(t *testing.T) {
	info, err := parseProtocolInfo([]string{
		`AUTH METHODS=COOKIE,SAFECOOKIE COOKIEFILE="/run/tor/control.authcookie"`,
		`VERSION Tor="0.4.8.9"`,
	})
	if err != nil {
		t.Fatalf("parseProtocolInfo: %v", err)
	}

	if want := []string{"COOKIE", "SAFECOOKIE"}; !slices.Equal(info.AuthMethods, want) {
		t.Errorf("AuthMethods = %q; want %q", info.AuthMethods, want)
	}
	if want := "/run/tor/control.authcookie"; info.AuthCookieFile != want {
		t.Errorf("AuthCookieFile = %q; want %q", info.AuthCookieFile, want)
	}
	if want := "0.4.8.9"; info.Version != want {
		t.Errorf("Version = %q; want %q", info.Version, want)
	}
}

func TestProtocolInfoHasAuthMethod(t *testing.T) {
	if (ProtocolInfo{}).HasAuthMethod("COOKIE") {
		t.Error("empty ProtocolInfo HasAuthMethod(COOKIE) = true; want false")
	}

	info := ProtocolInfo{AuthMethods: []string{"NULL", "COOKIE"}}
	if !info.HasAuthMethod("COOKIE") {
		t.Errorf("%q HasAuthMethod(COOKIE) = false; want true", info.AuthMethods)
	}
}

func TestPortsToString(t *testing.T) {
	if got := portsToString(nil); got != "" {
		t.Errorf("portsToString(nil) = %q; want empty", got)
	}

	if got, want := portsToString(Port(1791, "127.0.0.1:5000")), "Port=1791,127.0.0.1:5000"; got != want {
		t.Errorf("portsToString = %q; want %q", got, want)
	}
}

func TestConfigControlAddr(t *testing.T) {
	if got, want := (Config{}).getContolAddr(), "127.0.0.1:9051"; got != want {
		t.Errorf("default control addr = %q; want %q", got, want)
	}

	if got, want := (Config{ControlAddr: "10.0.0.1:1"}).getContolAddr(), "10.0.0.1:1"; got != want {
		t.Errorf("configured control addr = %q; want %q", got, want)
	}
}

func TestControlAddOnion(t *testing.T) {
	conn := newScriptedConn("250-ServiceID=abcdef\r\n250-PrivateKey=ED25519-V3:AAAA\r\n250 OK\r\n")

	onion, err := New(conn).AddOnion(KeyNewV3, Port(1791, "127.0.0.1:5000"))
	if err != nil {
		t.Fatalf("AddOnion: %v", err)
	}

	if want := (Onion{ServiceID: "abcdef", PrivateKey: "ED25519-V3:AAAA"}); onion != want {
		t.Errorf("AddOnion = %+v; want %+v", onion, want)
	}

	if got, want := conn.written.String(), "ADD_ONION NEW:ED25519-V3 Port=1791,127.0.0.1:5000\r\n"; got != want {
		t.Errorf("sent %q; want %q", got, want)
	}
}

func TestControlGetInfoAndConf(t *testing.T) {
	info, err := New(newScriptedConn("250-version=0.4.8.9\r\n250 OK\r\n")).GetInfo("version")
	if err != nil || info != "0.4.8.9" {
		t.Errorf("GetInfo(version) = (%q, %v); want (%q, nil)", info, err, "0.4.8.9")
	}

	conf, err := New(newScriptedConn("250 SocksPort=9050\r\n")).GetConf("SocksPort")
	if err != nil || conf != "9050" {
		t.Errorf("GetConf(SocksPort) = (%q, %v); want (%q, nil)", conf, err, "9050")
	}
}

func TestControlGetInfoErrorReply(t *testing.T) {
	if _, err := New(newScriptedConn("552 Unrecognized key\r\n")).GetInfo("bogus"); err == nil {
		t.Error("GetInfo on a 552 reply returned nil error; want an error")
	}
}
