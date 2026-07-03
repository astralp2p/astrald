package apphost

import (
	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

// GetWebOriginPermits returns the permits configured for a trusted web
// origin (config: trusted_web_sources), or nil when the origin is not trusted.
// Registration renders the permits as a node→app contract.
func (mod *Module) GetWebOriginPermits(origin string) (permits []*auth.Permit) {
	for _, pc := range mod.config.TrustedWebSources[origin] {
		permits = append(permits, &auth.Permit{
			Action:     astral.String8(pc.Action),
			Delegation: astral.Uint8(pc.Delegation),
		})
	}
	return
}
