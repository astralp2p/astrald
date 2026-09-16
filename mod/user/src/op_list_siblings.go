package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListSiblingsArgs struct {
	Out  string
	Zone astral.Zone
}

// OpListSiblings authorizes the caller under SeeSwarm, then streams the
// identities of all currently linked sibling nodes. Derives the context from the
// caller's identity and the requested zone.
func (mod *Module) OpListSiblings(ctx *astral.Context, q *routing.IncomingQuery, args opListSiblingsArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return q.RejectWithCode(4)
	}

	ctx, cancel := ctx.WithIdentity(q.Caller()).IncludeZone(args.Zone).WithCancel()
	defer cancel()

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, id := range mod.getSiblings() {
		err = ch.Send(id)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
