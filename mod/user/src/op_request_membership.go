package user

import (
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opRequestMembershipArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
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

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	target := q.Caller()
	joinAllowed := mod.GetSwarmJoinRequestPolicy()(target)
	if !joinAllowed {
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
