package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opGrantArgs struct {
	ID       *astral.Identity `query:"required"`
	Action   astral.String8   `query:"required"`
	Duration astral.Duration
	Out      string
}

// OpGrant records a node-local grant of one action for an identity, replacing
// whatever that identity held for the same action. An omitted duration grants
// until the grant is revoked.
//
// why: registration is otherwise the only writer of a grant, so an app that is
// already registered could reach a newly guarded op only by re-registering
// under a fresh identity or by being issued a signed contract.
func (mod *Module) OpGrant(ctx *astral.Context, q *routing.IncomingQuery, args opGrantArgs) error {
	// why: a grant authorizes on this node alone and is never handed to anyone,
	// so a caller off a link has no standing to write one.
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

	// why: the expiry is stored UTC because FindGrant compares it against a UTC
	// clock and the pure-Go sqlite driver compares datetimes lexically.
	var expiresAt *time.Time
	if args.Duration != 0 {
		t := time.Now().UTC().Add(args.Duration.Duration())
		expiresAt = &t
	}

	// note: the permit carries no constraints. A constrained permit is refused
	// rather than granted in full, so this op writes the action whole.
	if err := mod.Grant(args.ID, &auth.Permit{Action: args.Action}, expiresAt); err != nil {
		mod.log.Errorv(1, "error granting %v to %v: %v", args.Action, args.ID, err)
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "granted %v to %v", args.Action, args.ID)

	return ch.Send(&astral.Ack{})
}
