package user

import (
	"errors"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// LocalUser is the reserved name resolving to the user identity, as dir's
// localnode resolves to the node identity.
const LocalUser = "localuser"

var _ dirmod.Resolver = &Module{}

// ResolveIdentity resolves LocalUser to the user identity - the issuer of the
// active contract. Every other name is declined, which is how a resolver hands
// a name down the chain.
func (mod *Module) ResolveIdentity(s string) (*astral.Identity, error) {
	if s != LocalUser {
		return nil, errors.New("unknown name")
	}

	// why: dir takes any nil error as a hit, so an unclaimed node must error
	// rather than resolve the name to a nil identity
	id := mod.Identity()
	if id == nil {
		return nil, user.ErrNoActiveContract
	}

	return id, nil
}

// DisplayName returns an empty string. The user identity's display name is the
// alias dir already holds, and answering here would recurse back through Dir.
func (mod *Module) DisplayName(*astral.Identity) string { return "" }
