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
	// Permits names the actions asked for, comma-separated, as
	// apphost.register's grant_permits.
	Permits string `query:"required"`
	In      string
	Out     string
}

// OpRequestGrant asks the grant request policy to grant the calling session's
// identity the permits it names, and grants all of them on approval, until
// revoked.
func (mod *Module) OpRequestGrant(ctx *astral.Context, q *routing.IncomingQuery, args opRequestGrantArgs) error {
	// why before accepting: accepting drops the en-route entry naming the
	// session and its origin.
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
	permits := parsePermits(args.Permits)
	if len(permits) == 0 {
		return ch.Send(astral.NewError("missing permits"))
	}

	// why: an op runs on a context detached from the requester's, and only the
	// conn tells a waiting decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetGrantRequestPolicy()(policyCtx, &apphostapi.GrantRequest{
		Requester: requester, Origin: astral.String8(origin), Permits: permits,
	})
	switch {
	case err != nil:
		mod.log.Logv(1, "grant request from %v refused: %v", requester, err)
		return ch.Send(apphostapi.ErrGrantDeclined)
	case !bool(dec.Allow):
		mod.log.Logv(1, "grant request from %v refused by policy", requester)
		return ch.Send(apphostapi.ErrGrantDeclined)
	}

	for _, permit := range permits {
		if err = mod.Grant(requester, permit, nil); err != nil {
			return ch.Send(astral.Err(err))
		}
	}
	mod.log.Logv(1, "granted %v requested permits to %v", len(permits), requester)
	return ch.Send(&astral.Ack{})
}
