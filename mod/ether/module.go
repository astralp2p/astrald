package ether

import (
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "ether"

// Module provides public communication within various broadcast networks such as local area networks.
type Module interface {
	// Push broadcasts an object to the ether, signed by and sourced from the node's identity.
	Push(astral.Object) error
	// PushToIP sends an object to one IP address, signed by and sourced from the node's identity.
	PushToIP(ip.IP, astral.Object) error
}
