package astrald

import "github.com/cryptopunkscc/astral-go/astral"

// NewContext returns a context pre-populated with the default client's guest identity and ZoneAll,
// suitable for outbound queries without additional configuration.
func NewContext() *astral.Context {
	return astral.
		NewContext(nil).
		WithIdentity(GuestID()).
		WithZone(astral.ZoneAll)
}
