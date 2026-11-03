package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListGrantsArgs struct {
	Identity string `query:"required"`
	Out      string
}

// OpListGrants lists the node-local grants an identity currently holds, one
// mod.apphost.grant per grant with its ID, permit and expiry, then eos. A grant
// that lasts until revoked carries no expiry.
//
// why: the listing drops an expired grant the way the authorizer does, so it
// answers what authorizes now.
func (mod *Module) OpListGrants(ctx *astral.Context, q *routing.IncomingQuery, args opListGrantsArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why: the grant table is this node's private answer about local authority,
	// so it is not enumerable over a link.
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if identity.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	grants, err := mod.activeGrants(identity)
	if err != nil {
		mod.log.Errorv(1, "error listing grants of %v: %v", identity, err)
		return ch.Send(astral.Err(err))
	}

	for _, grant := range grants {
		if err = ch.Send(grant); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
