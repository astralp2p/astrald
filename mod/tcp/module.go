package tcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/exonet"
)

const ModuleName = "tcp"

// Module is the public contract for the TCP transport.
type Module interface {
	exonet.Dialer
	exonet.Unpacker
	exonet.Parser
	ListenPort() int
	CreateEphemeralListener(ctx *astral.Context, port astral.Uint16, handler exonet.EphemeralHandler) error
}
