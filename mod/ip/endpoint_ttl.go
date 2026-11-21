package ip

import (
	"time"

	"github.com/astralp2p/astral-go/api/ip"
)

const (
	// PublicEndpointTTL is the TTL of an advertised endpoint on a public IP.
	PublicEndpointTTL = 7 * 24 * time.Hour
	// LocalEndpointTTL is the TTL of an advertised endpoint on any other IP:
	// private, CGNAT, ULA and link-local.
	// why: a peer holds a local row only while it can renew it from this node;
	// a row of a network this node left lapses within hours, not days.
	LocalEndpointTTL = 2 * time.Hour
)

// EndpointTTL returns the TTL this node advertises its endpoint on a with.
func EndpointTTL(a ip.IP) time.Duration {
	if a.IsPublic() {
		return PublicEndpointTTL
	}
	return LocalEndpointTTL
}
