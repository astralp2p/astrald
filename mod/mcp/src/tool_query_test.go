package mcp

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

func collectConn(t *testing.T) (astral.Conn, net.Conn) {
	t.Helper()
	local, remote := net.Pipe()
	return testConn{Conn: local, local: astral.GenerateIdentity(), remote: astral.GenerateIdentity()}, remote
}

func TestCollectResponseAutoText(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		peer.Write([]byte("plain text answer"))
		peer.Close()
	}()

	out := mod.collectResponse(conn, "", time.Second)
	if out.Payload != "plain text answer" || out.Encoding != "utf8" {
		t.Fatalf("payload %q (%v)", out.Payload, out.Encoding)
	}
	if len(out.Objects) != 0 {
		t.Fatalf("text misread as %v objects", len(out.Objects))
	}
}

func TestCollectResponseAutoObjects(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		sender := channel.NewSender(peer)
		sender.Send(&astral.Ack{})
		sender.Send(&astral.EOS{})
		peer.Close()
	}()

	out := mod.collectResponse(conn, "", time.Second)
	if len(out.Objects) != 1 {
		t.Fatalf("%v objects, want 1 (payload %q)", len(out.Objects), out.Payload)
	}
	if out.Payload != "" {
		t.Fatalf("framed stream also produced payload %q", out.Payload)
	}
}

func TestCollectResponseForcedRaw(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		sender := channel.NewSender(peer)
		sender.Send(&astral.Ack{})
		peer.Close()
	}()

	out := mod.collectResponse(conn, sessionFormatRaw, time.Second)
	if len(out.Objects) != 0 {
		t.Fatalf("raw mode decoded %v objects", len(out.Objects))
	}
	if out.Payload == "" {
		t.Fatal("raw mode returned no payload")
	}
}
