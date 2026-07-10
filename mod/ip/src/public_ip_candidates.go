package ip

import (
	"github.com/astralp2p/astral-go/api/ip"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

func (mod *Module) AddPublicIPCandidateProvider(provider ipmod.PublicIPCandidateProvider) error {
	return mod.providers.Add(provider)
}

// PublicIPCandidates collects candidates from all registered providers, deduplicating by string representation.
func (mod *Module) PublicIPCandidates() (ips []ip.IP) {
	var unique = map[string]struct{}{}

	for _, provider := range mod.providers.Clone() {
		for _, i := range provider.PublicIPCandidates() {
			if _, found := unique[i.String()]; found {
				continue
			}

			unique[i.String()] = struct{}{}
			ips = append(ips, i)
		}
	}

	return
}
