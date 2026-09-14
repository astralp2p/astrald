package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListGrantsArgs struct {
	ID  *astral.Identity `query:"required"`
	Out string
}

// OpListGrants lists the node-local grants an identity currently holds.
//
// why: the listing drops an expired grant the way the authorizer does, so it
// answers what authorizes now. A permit carries no expiry of its own, so a
// listing that included lapsed rows could not mark them as lapsed.
func (mod *Module) OpListGrants(ctx *astral.Context, q *routing.IncomingQuery, args opListGrantsArgs) error {
	// why: the grant table is this node's private answer about local authority,
	// so it is not enumerable over a link.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.authorizeAdminManageApps(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.ID.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	permits, err := mod.activeGrants(args.ID)
	if err != nil {
		mod.log.Errorv(1, "error listing grants of %v: %v", args.ID, err)
		return ch.Send(astral.Err(err))
	}

	for _, permit := range permits {
		if err = ch.Send(permit); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
