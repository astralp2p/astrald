package noise

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// plainConn is a transport conn that sets no keepalive.
type plainConn struct{ exonetmod.Conn }

// timedConn is a transport conn that sets keepalive k.
type timedConn struct {
	plainConn
	k exonetmod.LinkKeepalive
}

func (c timedConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.k }
func (plainConn) RemoteEndpoint() exonet.Endpoint          { return nil }

// TestConnPassesOnTransportKeepalive: the handshake conn reports the timing of
// the transport conn under it, and zero for a transport that sets none.
func TestConnPassesOnTransportKeepalive(t *testing.T) {
	k := exonetmod.LinkKeepalive{Idle: 7 * time.Second, Timeout: 3 * time.Second}
	if got := (&Conn{conn: timedConn{k: k}}).LinkKeepalive(); got != k {
		t.Fatalf("LinkKeepalive() over a timed transport = %v; want %v", got, k)
	}
	if got := (&Conn{conn: plainConn{}}).LinkKeepalive(); got != (exonetmod.LinkKeepalive{}) {
		t.Fatalf("LinkKeepalive() over a transport with no timing = %v; want zero", got)
	}
}
