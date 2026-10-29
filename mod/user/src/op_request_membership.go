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
	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	ac := mod.ActiveContract()

	if ac == nil {
		// We don't have an active contract to invite
		return ch.Send(user.ErrNoActiveContract)
	}

	target := q.Caller()

	// why: the requester sends nothing more, and only the conn tells a waiting
	// decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetSwarmJoinPolicy()(policyCtx, &user.SwarmJoinRequest{Requester: target})
	switch {
	case err != nil:
		mod.log.Logv(1, "join request from %v declined: %v", target, err)
		return ch.Send(user.ErrRequestDeclined)
	case !bool(dec.Allow):
		return ch.Send(user.ErrRequestDeclined)
	}

	signed, err := mod.IssueMembership(ctx, target)
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
