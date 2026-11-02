package apphost

import (
	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

type opRequestGrantArgs struct {
	In  string
	Out string
}

// OpRequestGrant reads one mod.apphost.request_grant_request after accept and
// asks the grant request policy to grant the calling session's identity the
// permits it carries, constraints included. On approval every permit is added
// beside the grants the identity already holds, for the requested duration; a
// zero duration grants until revoked. The policy is asked on every call, even
// when the identity already holds every requested permit.
func (mod *Module) OpRequestGrant(ctx *astral.Context, q *routing.IncomingQuery, args opRequestGrantArgs) error {
	// why before accepting: accepting drops the en-route entry naming the session and its origin.
	requester := mod.sessionOwner(q)
	origin, _ := mod.EnRouteQueryExtras(q.Nonce())[apphostmod.ExtraOriginWeb].(string)

	// why accept before deciding: a delegate may wait on a person.
	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: a grant authorizes on this node alone, and a link query could share
	// a guest's nonce before the core router claims it (core/router.go).
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}
	// why the session and not q.Caller(): a token-less caller arrives as the
	// node identity, and a sudo caller is not the identity that holds the token.
	if requester.IsZero() {
		return ch.Send(auth.ErrNotPermitted)
	}
	if err := refuseRemovedArgs(q.QueryString(), "permits"); err != nil {
		return ch.Send(astral.Err(err))
	}

	var req *apphostapi.RequestGrantRequest
	if err := ch.Switch(channel.Expect(&req), channel.FailOnEOF()); err != nil {
		return ch.Send(astral.Err(err))
	}
	switch {
	case len(req.Permits) == 0:
		return ch.Send(astral.NewError("missing permits"))
	case req.Duration < 0:
		return ch.Send(astral.NewError("negative duration"))
	}
	if err := validatePermits(req.Permits); err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: an op runs on a context detached from the requester's, and only the
	// conn tells a waiting decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetGrantRequestPolicy()(policyCtx, &apphostapi.GrantPolicyRequest{
		Requester: requester, Origin: astral.String8(origin), Permits: req.Permits, Duration: req.Duration,
	})
	switch {
	case err != nil:
		mod.log.Logv(1, "grant request from %v refused: %v", requester, err)
		return ch.Send(apphostapi.ErrGrantDeclined)
	case !bool(dec.Allow):
		mod.log.Logv(1, "grant request from %v refused by policy", requester)
		return ch.Send(apphostapi.ErrGrantDeclined)
	}

	// why after the decision: a delegate that waits on a person does not
	// shorten the duration the session asked for.
	if err = mod.grantAll(requester, req.Permits, grantExpiry(req.Duration)); err != nil {
		return ch.Send(astral.Err(err))
	}
	mod.log.Logv(1, "granted %v requested permits to %v", len(req.Permits), requester)
	return ch.Send(&astral.Ack{})
}
