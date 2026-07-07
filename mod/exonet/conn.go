package exonet

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	"io"
)

// Conn represents a raw, unencrypted, unauthenticated network connection
type Conn interface {
	io.ReadWriteCloser               // Basic IO operations
	Outbound() bool                  // Returns true if we are the active party, false otherwise
	LocalEndpoint() exonet.Endpoint  // Returns local network address if known, nil otherwise
	RemoteEndpoint() exonet.Endpoint // Returns the other party's network address if known, nil otherwise
}
