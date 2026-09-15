package apphost

/*
	op cancel cancels an en route query
*/

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCancelArgs struct {
	QueryID astral.Nonce `query:"required"`
	Cause   *string
	Out     string
}

func (mod *Module) OpCancel(ctx *astral.Context, q *routing.IncomingQuery, args opCancelArgs) (err error) {
	// why: an entry launched by a token-less session records no owner and stays
	// cancellable by any caller, so ownership alone leaves it reachable from a
	// link. apphost.bind and apphost.register_handler refuse the origin outright.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	// why: both lookups run before accepting - accepting resolves this query and
	// drops the en-route entry that names the session behind it.
	owner := mod.sessionOwner(q)
	enRoute, found := mod.enRoute.Get(args.QueryID)
	allowed := found && mod.mayCancel(ctx, owner, enRoute)

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// note: a query this session may not cancel answers as a missing one, so a
	// caller learns nothing about the nonces other apps hold en route.
	if !allowed {
		return ch.Send(astral.NewError("query not found"))
	}

	if args.Cause == nil || len(*args.Cause) == 0 {
		enRoute.cancel(nil)
	} else {
		enRoute.cancel(astral.NewError(*args.Cause))
	}

	mod.log.Logv(2, "cancelled query %v", args.QueryID)

	return ch.Send(&astral.Ack{})
}

// mayCancel reports whether principal may cancel the en-route entry.
//
// note: an entry launched by a token-less session records no owner and stays
// cancellable by any local session, which is what apphost.cancel did before
// ownership. Only an authenticated app's query gains a guard.
func (mod *Module) mayCancel(ctx *astral.Context, principal *astral.Identity, e *queryEnRoute) bool {
	if e.owner.IsZero() {
		return true
	}

	if principal.IsEqual(e.owner) {
		return true
	}

	return mod.mayManageApps(ctx, principal)
}
