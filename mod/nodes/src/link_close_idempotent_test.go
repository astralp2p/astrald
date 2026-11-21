package nodes

import (
	"errors"
	"net"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// plainLink runs a link over conn; closing the link closes conn, so the peer sees EOF.
func plainLink(t *testing.T, conn net.Conn, outbound bool) *Link {
	t.Helper()
	mod := &Module{log: log.New(astral.Anyone)}
	c := keepaliveConn{pipeIdentityConn{conn}, quietKeepalive}
	link := newLink(mod, c, astral.NewNonce(), outbound)
	t.Cleanup(func() { link.Close() })
	return link
}

// TestCloseOfClosedLinkSucceeds: closing a link that the peer already closed
// succeeds, so nodes.close_link answers ack for a link still in the pool
// (astral-docs protocols/nodes/ops/nodes.close_link.md: an error only if no link with that id exists).
func TestCloseOfClosedLinkSucceeds(t *testing.T) {
	ca, cb := tcpPair(t)
	a, b := plainLink(t, ca, true), plainLink(t, cb, false)
	if err := b.CloseWithError(errors.New("link closed")); err != nil {
		t.Fatalf("first close: %v", err)
	}
	waitDone(t, a, peerCloseBound, "peer after the close")
	if err := a.CloseWithError(errors.New("link closed")); err != nil {
		t.Fatalf("closing a link the peer closed: %v; want nil", err)
	}
	if err := b.CloseWithError(errors.New("link closed")); err != nil {
		t.Fatalf("second close of the same link: %v; want nil", err)
	}
}
