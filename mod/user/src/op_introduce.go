package user

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// introducePushTimeout bounds the push of the active contract to the introduced node.
const introducePushTimeout = 30 * time.Second

type opIntroduceArgs struct {
	Identity string `query:"required"`
	In       string
	Out      string
}

// OpIntroduce pushes this node's active contract to the named node, then syncs
// with it. The named node indexes the contract, so this node joins its local
// swarm; the sync carries the rest of the swarm. The reply is an ack.
// Requires an active contract; the caller must be authorized for
// user.AdminSwarmAction over the named node and must not be expelled
// (auth.ErrNotPermitted otherwise).
// Spec: .ai/system/protocols/user/ops/user.introduce.md
//
// why no network origin: a swarm member could otherwise relay the query as the
// user and make this node push to any target, also through an expelled node's
// delegated authority.
// note: shell.shell launches its queries without an origin
// (mod/shell/src/session.go), so a member that relays shell.shell as the user
// still reaches this op.
func (mod *Module) OpIntroduce(ctx *astral.Context, q *routing.IncomingQuery, args opIntroduceArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	ac := mod.ActiveContract()
	if ac == nil {
		return ch.Send(user.ErrNoActiveContract)
	}

	// why: the action carries the node, so resolution runs before authorization.
	nodeID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if nodeID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}
	if nodeID.IsEqual(mod.node.Identity()) || nodeID.IsEqual(ac.Issuer) {
		return ch.Send(user.ErrIntroduceSelf)
	}

	// why the ban check: the authorizer chain walk ignores bans, so an expelled
	// management node keeps AdminSwarm through delegation.
	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(q.Caller()),
		Subject: nodeID,
	}) || mod.isExpelled(ac.Issuer, q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if err := mod.introduce(ctx, ac, nodeID); err != nil {
		return ch.Send(err)
	}

	return ch.Send(&astral.Ack{})
}

// introduce pushes the active contract ac to nodeID and schedules a sync with nodeID.
func (mod *Module) introduce(ctx *astral.Context, ac *auth.SignedContract, nodeID *astral.Identity) astral.Error {
	if ac.ExpiresAt.Time().Before(time.Now()) {
		return astral.Err(auth.ErrContractExpired)
	}
	if mod.isExpelled(ac.Issuer, nodeID) {
		return user.ErrExpelled
	}

	// why: the push is this node's own query, and a session without a token
	// has the Network zone stripped (mod/apphost/src/guest.go).
	pctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(introducePushTimeout)
	defer cancel()

	if err := mod.Objects.Push(pctx, nodeID, ac); err != nil {
		return astral.Err(fmt.Errorf("push to %v: %w", nodeID, err))
	}

	// why: the link sync (object_receiver.go) skips nodeID, since this node holds
	// no contract for nodeID until nodeID's own sync pushes one.
	mod.Scheduler.Schedule(mod.NewSyncNodesTask(nodeID))

	return nil
}
