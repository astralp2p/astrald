package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"gorm.io/gorm"
)

type opRevokeArgs struct {
	Identity string `query:"required"`
	// ID names one grant, as apphost.list_grants reports it.
	ID astral.Uint64
	// Action names every grant the identity holds for the action.
	Action astral.String8
	Out    string
}

// OpRevoke withdraws an identity's node-local grants: the one grant ID names,
// or every grant for Action. Exactly one of the two is given. The withdrawal
// takes effect on the next authorization and does not undo what the identity
// did while it held the grant.
func (mod *Module) OpRevoke(ctx *astral.Context, q *routing.IncomingQuery, args opRevokeArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why: a grant authorizes on this node alone, so a caller off a link has no
	// standing to withdraw one.
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

	switch {
	case args.ID != 0 && args.Action != "":
		return ch.Send(astral.NewError("give id or action, not both"))
	case args.ID == 0 && args.Action == "":
		return ch.Send(astral.NewError("missing id or action"))
	}

	// note: grant IDs start at 1, so a zero ID is an absent argument.
	if args.ID != 0 {
		err = mod.RevokeGrant(identity, uint64(args.ID))
	} else {
		err = mod.Revoke(identity, string(args.Action))
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ch.Send(astral.NewError("grant not found"))
		}
		mod.log.Errorv(1, "error revoking id %v action %v from %v: %v", args.ID, args.Action, identity, err)
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "revoked id %v action %v from %v", args.ID, args.Action, identity)

	return ch.Send(&astral.Ack{})
}
