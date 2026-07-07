package ether

import (
	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
)

const ModuleName = "ether"

// Module provides public communication within various broadcast networks such as local area networks.
type Module interface {
	// Push an object to the ether
	Push(astral.Object, *astral.Identity) error
	PushToIP(ip.IP, astral.Object, *astral.Identity) error
}
