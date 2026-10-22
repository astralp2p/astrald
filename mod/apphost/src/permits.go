package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// isTrustedWebSource reports whether trusted_web_sources lists origin, with or
// without permits.
func (mod *Module) isTrustedWebSource(origin string) bool {
	_, ok := mod.config.TrustedWebSources[origin]
	return ok
}

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
