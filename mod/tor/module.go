package tor

import (
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

// Module is the contract a Tor integration must satisfy: it can dial outbound connections,
// unpack binary endpoint representations, and parse human-readable Tor addresses.
type Module interface {
	exonet.Dialer
	exonet.Unpacker
	exonet.Parser
}
