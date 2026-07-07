package nat

import (
	"context"

	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Puncher is a minimal abstraction for UDP NAT hole punching.
// Implementations supply configuration (e.g., ports, timeouts) outside this interface.
type Puncher interface {
	// Open binds a local UDP socket and returns the chosen local port.
	// The socket is kept by the puncher and reused by HolePunch.
	Open() (localPort int, err error)
	// HolePunch attempts a UDP punch towards the given peer IP and port.
	// Returns a PunchResult with information about the punch outcome.
	HolePunch(ctx context.Context, peerIP ip.IP, peerPort int) (*PunchResult, error)
	// Close releases any resources held by the puncher (open sockets).
	Close() error

	// Session returns the opaque byte sequence embedded in punch packets to
	// distinguish this puncher's traffic from other concurrent punch attempts.
	Session() []byte
}

// PunchResult contains the outcome of a successful UDP hole punch.
//
// It reports only what *this side* observed for the peer (the remote
// IP and port that responded with the same session payload).
// The peer’s observation of our own external port, if needed,
// must be exchanged separately via signaling.
type PunchResult struct {
	LocalPort  astral.Uint16
	RemoteIP   ip.IP
	RemotePort astral.Uint16
}
