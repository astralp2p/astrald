package kcp

import (
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

const ModuleName = "kcp"

// Module is the public contract for the KCP transport: it dials, unpacks, and parses
// exonet endpoints over KCP/UDP and exposes the port it is bound to.
type Module interface {
	exonet.Dialer
	exonet.Unpacker
	exonet.Parser
	// ListenPort returns the UDP port the module is currently bound to; 0 means unbound.
	ListenPort() int
}
