package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRequestMembershipArgs struct {
	In  string
	Out string
}

// OpRequestMembership allows a caller node to request membership in this node's swarm.
// Requires an active contract; applies the swarm join-request policy to the caller before issuing membership.
// Pushes the resulting signed contract to the local swarm asynchronously.
func (mod *Module) OpRequestMembership(ctx *astral.Context, q *routing.IncomingQuery, args opRequestMembershipArgs) (err error) {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	ac := mod.ActiveContract()

	if ac == nil {
		// We don't have an active contract to invite
		return q.RejectWithCode(2)
	}

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why always: besides this node's join policy, issuing the membership waits
	// on the requester node's invite policy, which may be delegated there, and
	// only the conn tells either wait that nobody waits for the membership any
	// more.
	policyCtx, stop := watchRequester(ctx, conn)
	defer stop()

	target := q.Caller()
	joinAllowed := mod.GetSwarmJoinRequestPolicy()(policyCtx, target)
	if !joinAllowed {
		return ch.Send(user.ErrRequestDeclined)
	}

	// why: an approval that arrives after the requester left would issue a
	// membership nobody asked for any more.
	if policyCtx.Err() != nil {
		mod.log.Logv(1, "join of %v approved after the requester left; not issuing", target)
		return nil
	}

	signed, err := mod.IssueMembership(policyCtx, target)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	err = mod.Auth.IndexContract(ctx, signed)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	go mod.PushToLocalSwarm(mod.ctx, signed)

	// why: PushToLocalSwarm only sends the new contract, leaving the invitee
	// without the inviter's own and sibling contracts. The LinkCreatedEvent
	// trigger already fired before indexing, so sync the joined node here.
	mod.Scheduler.Schedule(mod.NewSyncNodesTask(signed.Subject))

	return ch.Send(signed)
}
