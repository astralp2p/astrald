package ether

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/ip"
)

const ModuleName = "ether"

// Module provides public communication within various broadcast networks such as local area networks.
type Module interface {
	// Push an object to the ether
	Push(astral.Object, *astral.Identity) error
	PushToIP(ip.IP, astral.Object, *astral.Identity) error
}
