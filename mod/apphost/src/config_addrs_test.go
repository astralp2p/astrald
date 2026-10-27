package apphost

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func TestResolveAddrs(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	logger := log.New(nil)
	logger.SetFilter(func(*log.Entry) bool { return false })

	mod := &Module{config: defaultConfig, log: logger}
	mod.config.Listen = []string{"tcp:127.0.0.1:8625", "unix:~/.apphost.sock", "unix:rel.sock", "unix:/run/a.sock", "memu:apphosty"}
	mod.config.BindHTTP = "unix:~/http.sock"

	mod.resolveAddrs()

	want := []string{"tcp:127.0.0.1:8625", "unix:/home/u/.apphost.sock", "unix:/run/a.sock", "memu:apphosty"}
	if !reflect.DeepEqual(mod.config.Listen, want) {
		t.Fatalf("listen = %q, want %q", mod.config.Listen, want)
	}
	if mod.config.BindHTTP != "unix:/home/u/http.sock" {
		t.Fatalf("bind_http = %q, want unix:/home/u/http.sock", mod.config.BindHTTP)
	}
	if defaultConfig.Listen[1] != "unix:~/.apphost.sock" {
		t.Fatalf("resolveAddrs changed the default listen list: %q", defaultConfig.Listen)
	}
}

func TestResolveAddrsDisablesRelativeBindHTTP(t *testing.T) {
	logger := log.New(nil)
	logger.SetFilter(func(*log.Entry) bool { return false })

	mod := &Module{config: defaultConfig, log: logger}
	mod.config.BindHTTP = "unix:rel.sock"

	mod.resolveAddrs()

	if mod.config.BindHTTP != "" {
		t.Fatalf("bind_http = %q, want empty (HTTP disabled)", mod.config.BindHTTP)
	}
}

// TestRegisterHandlerRefusesRelativeUnixEndpoint: a relative unix: endpoint installs no handler.
func TestRegisterHandlerRefusesRelativeUnixEndpoint(t *testing.T) {
	mod, _ := ownershipModule(true)
	session := astral.GenerateIdentity()

	for _, endpoint := range []string{"unix:rel.sock", "unix:~/app.sock"} {
		w := newRecordingWriter()
		q := sessionQuery(mod, session, session,
			"apphost.register_handler?endpoint="+endpoint+"&token="+astral.NewNonce().String())
		if err := routeQuery(t, mod.OpRegisterHandler, q, w); err != nil {
			t.Fatalf("apphost.register_handler(%v): %v", endpoint, err)
		}
		awaitOwnershipAnswer(t, w)
	}

	if n := len(mod.ipcHandlers.Clone()); n != 0 {
		t.Fatalf("apphost.register_handler installed %d handlers for relative endpoints; want 0", n)
	}
}
