package nearby

import (
	"errors"
	dirmod "github.com/cryptopunkscc/astrald/mod/dir"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/dir"
	"github.com/cryptopunkscc/astral-go/astral"
)

var _ dirmod.Resolver = &Module{}

// ResolveIdentity resolves a dot-prefixed alias (e.g. ".phone") by scanning the
// nearby cache for a matching dir.Alias attachment; returns an error if no peer
// has announced that alias or the prefix is absent.
func (mod *Module) ResolveIdentity(s string) (*astral.Identity, error) {
	s, found := strings.CutPrefix(s, aliasPrefix)
	if !found {
		return nil, errors.New("not found")
	}

	for _, v := range mod.Cache().Clone() {
		if v.GetIdentity() == nil {
			continue
		}

		alias, ok := astral.First[*dir.Alias](v.Status.Attachments.Objects())
		if ok && alias != nil && alias.String() == s {
			return v.GetIdentity(), nil
		}

	}

	return nil, errors.New("not found")
}

func (mod *Module) DisplayName(identity *astral.Identity) string {
	return ""
}
