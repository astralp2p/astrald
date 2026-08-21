package ip

import "errors"
import (
	"github.com/astralp2p/astral-go/api/ip"
)

const ModuleName = "ip"

// Module is the public API of the ip module; LocalIPs returns addresses bound to local interfaces,
// while PublicIPCandidates returns the subset considered reachable from the internet.
type Module interface {
	LocalIPs() ([]ip.IP, error)
	PublicIPCandidates() []ip.IP
	DefaultGateway() (ip.IP, error)
}

type PublicIPCandidateProvider interface {
	PublicIPCandidates() []ip.IP // sorted
}

var ErrDefaultGatewayNotFound = errors.New("default gateway not found")
