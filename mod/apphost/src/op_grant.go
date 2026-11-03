package apphost

import (
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opGrantArgs struct {
	In  string
	Out string
}

// OpGrant reads one mod.apphost.grant_request after accept and adds every
// permit it carries as a node-local grant for its identity, beside the grants
// that identity already holds; an identical permit refreshes its expiry. A
// zero duration grants until revoked. A constraint on an action that ignores
// constraints is written as sent and has no effect. The op answers ack.
//
// why: registration is otherwise the only writer of a grant, so an app that is
// already registered could reach a newly guarded op only by re-registering
// under a fresh identity or by being issued a signed contract.
func (mod *Module) OpGrant(ctx *astral.Context, q *routing.IncomingQuery, args opGrantArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: a grant authorizes on this node alone and is never handed to anyone,
	// so a caller off a link has no standing to write one.
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if err := refuseRemovedArgs(q.QueryString(), "identity", "action", "duration", "constrained"); err != nil {
		return ch.Send(astral.Err(err))
	}

	var req *apphostapi.GrantRequest
	if err := ch.Switch(channel.Expect(&req), channel.FailOnEOF()); err != nil {
		return ch.Send(astral.Err(err))
	}

	switch {
	case req.Identity == nil || req.Identity.IsZero():
		return ch.Send(astral.NewError("missing identity"))
	case len(req.Permits) == 0:
		return ch.Send(astral.NewError("missing permits"))
	case req.Duration < 0:
		return ch.Send(astral.NewError("negative duration"))
	}

	if err := validatePermits(req.Permits); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.grantAll(req.Identity, req.Permits, grantExpiry(req.Duration)); err != nil {
		mod.log.Errorv(1, "error granting %v permits to %v: %v", len(req.Permits), req.Identity, err)
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "granted %v permits to %v", len(req.Permits), req.Identity)

	return ch.Send(&astral.Ack{})
}

// grantExpiry returns the expiry of a grant that lasts d from now, or nil for
// a zero d, which grants until revoked.
//
// why: the expiry is stored UTC because FindGrants compares it against a UTC
// clock and the pure-Go sqlite driver compares datetimes lexically.
func grantExpiry(d astral.Duration) *time.Time {
	if d == 0 {
		return nil
	}
	t := time.Now().UTC().Add(d.Duration())
	return &t
}
