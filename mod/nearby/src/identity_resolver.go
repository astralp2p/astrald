package nearby

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

var _ dirmod.Resolver = &Module{}

// ResolveIdentity resolves a dot-prefixed alias (e.g. ".phone") to the verified cached
// peer whose public profile names that alias; it returns an error when the prefix is
// absent or no verified peer names the alias.
func (mod *Module) ResolveIdentity(s string) (*astral.Identity, error) {
	name, found := strings.CutPrefix(s, aliasPrefix)
	if !found || name == "" {
		return nil, errors.New("not found")
	}

	for _, e := range mod.cache.live(mod.now()) {
		if e.NodeID == nil {
			continue
		}

		profile, ok := astral.First[*nearby.PublicProfile](e.Status.Attachments.Objects())
		if ok && profile != nil && string(profile.NodeAlias) == name {
			return e.NodeID, nil
		}
	}

	return nil, errors.New("not found")
}

func (mod *Module) DisplayName(identity *astral.Identity) string {
	return ""
}
