package apphost

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// A caller that gives up before the dial completes must not cost a live app
// its handler: only an endpoint that cannot be reached is removed.
func TestAbandonedDialKeepsTheHandler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	app := astral.GenerateIdentity()
	handler := &IPCHandler{Identity: app, Owner: app, IPCToken: astral.NewNonce(), Endpoint: "unix:" + path}

	mod := &Module{log: log.New(nil)}
	mod.ipcHandlers.Add(handler)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	_, _ = mod.RouteQuery(ctx, astral.Launch(query.New(astral.GenerateIdentity(), app, "x", nil)), newRecordingWriter())

	if !mod.ipcHandlers.Contains(handler) {
		t.Fatal("an abandoned dial removed a live app's handler")
	}
}

// An endpoint nobody listens on is still removed.
func TestUnreachableEndpointRemovesTheHandler(t *testing.T) {
	app := astral.GenerateIdentity()
	handler := &IPCHandler{Identity: app, Owner: app, IPCToken: astral.NewNonce(), Endpoint: "unix:" + filepath.Join(t.TempDir(), "gone.sock")}

	mod := &Module{log: log.New(nil)}
	mod.ipcHandlers.Add(handler)

	_, _ = mod.RouteQuery(astral.NewContext(nil), astral.Launch(query.New(astral.GenerateIdentity(), app, "x", nil)), newRecordingWriter())

	if mod.ipcHandlers.Contains(handler) {
		t.Fatal("an unreachable endpoint kept its handler")
	}
}
