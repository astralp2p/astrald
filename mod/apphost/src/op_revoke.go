package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"gorm.io/gorm"
)

type opRevokeArgs struct {
	ID     *astral.Identity `query:"required"`
	Action astral.String8   `query:"required"`
	Out    string
}

// OpRevoke withdraws an identity's node-local grant for one action. The
// withdrawal takes effect on the next authorization and does not undo what the
// identity did while it held the grant.
func (mod *Module) OpRevoke(ctx *astral.Context, q *routing.IncomingQuery, args opRevokeArgs) error {
	// why: a grant authorizes on this node alone, so a caller off a link has no
	// standing to withdraw one.
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

	if args.Action == "" {
		return ch.Send(astral.NewError("missing action"))
	}

	if err := mod.Revoke(args.ID, string(args.Action)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ch.Send(astral.NewError("grant not found"))
		}
		mod.log.Errorv(1, "error revoking %v from %v: %v", args.Action, args.ID, err)
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "revoked %v from %v", args.Action, args.ID)

	return ch.Send(&astral.Ack{})
}
