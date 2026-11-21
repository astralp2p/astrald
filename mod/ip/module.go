package ip

import (
	"errors"
	"net"

	"github.com/astralp2p/astral-go/api/ip"
)

const ModuleName = "ip"

// Module is the public API of the ip module; LocalIPs returns the advertisable addresses bound to
// local interfaces (no IPv6 link-local, no container or VM bridge addresses),
// while PublicIPCandidates returns the subset considered reachable from the internet.
// LocalSubnets returns the subnet of every non-loopback interface address.
type Module interface {
	LocalIPs() ([]ip.IP, error)
	LocalSubnets() ([]*net.IPNet, error)
	PublicIPCandidates() []ip.IP
	DefaultGateway() (ip.IP, error)
}

type PublicIPCandidateProvider interface {
	PublicIPCandidates() []ip.IP // sorted
}

var ErrDefaultGatewayNotFound = errors.New("default gateway not found")
