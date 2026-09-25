package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// AuthorizeHostMailbox is the root every hosting chain ends at: an identity may
// host its own mailbox and nobody else's.
//
// A node holds the action for a mailbox only under a contract the mailbox
// identity issued to it. auth re-enters a chain as each contract's issuer, and
// only the mailbox identity passes here, so a contract from any other identity,
// or one a node issues to itself, authorizes nothing.
//
// why the zero mailbox is refused before the self test: Identity.IsEqual
// answers true when both sides are zero, and the anonymous identity holds no
// mailbox.
func (mod *Module) AuthorizeHostMailbox(_ *astral.Context, a *messaging.HostMailboxAction) bool {
	return !a.MailboxID.IsZero() && a.Actor().IsEqual(a.MailboxID)
}

// addAuthorizers registers the rules this module answers for with auth.
//
// why the hosting root is not NodeLocal: its authority is a contract any node
// can verify, so a chain of contracts must be able to reach it.
func (mod *Module) addAuthorizers() {
	mod.Auth.Add(authmod.Func[*messaging.HostMailboxAction](mod.AuthorizeHostMailbox))
}
