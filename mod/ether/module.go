package ether

import (
	"errors"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "ether"

// ErrNotOnLink is returned when no local IPv4 subnet contains an IP.
var ErrNotOnLink = errors.New("ip is not on a local subnet")

// ErrPacketTooLarge is returned when an object does not fit one ether datagram.
var ErrPacketTooLarge = errors.New("packet too large")

// Module carries public objects to the nodes on the local IPv4 subnets.
// A datagram is unsigned and names no source identity; a receiver learns only its source IP.
type Module interface {
	// Push broadcasts an object on every local IPv4 subnet.
	Push(astral.Object) error
	// PushToSubnetOf broadcasts an object on the local IPv4 subnet that contains src only.
	// It returns ErrNotOnLink when no local IPv4 subnet contains src.
	PushToSubnetOf(src ip.IP, obj astral.Object) error
	// SubnetOf returns the CIDR of the local IPv4 subnet that contains src.
	SubnetOf(src ip.IP) (subnet string, ok bool)
}
