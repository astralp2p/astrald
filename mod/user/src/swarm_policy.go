package user

import (
	"context"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/user"
)

// GetSwarmJoinRequestPolicy returns the swarm join delegate's policy when one is
// set, accept-all otherwise. It is read on every request, so setting or clearing
// the delegate takes effect on the next.
func (mod *Module) GetSwarmJoinRequestPolicy() user.SwarmJoinRequestPolicy {
	if d := mod.config.SwarmJoinDelegate.Get(); d != nil && !d.IsZero() {
		return mod.SwarmJoinViaDelegate
	}
	return mod.SwarmJoinRequestAcceptAll
}

var _ user.SwarmJoinRequestPolicy = (*Module)(nil).SwarmJoinRequestAcceptAll
var _ user.SwarmJoinRequestPolicy = (*Module)(nil).SwarmJoinViaDelegate

func (mod *Module) SwarmJoinRequestAcceptAll(_ *astral.Context, requester *astral.Identity) bool {
	mod.log.Info("Accepting %v join request into swarm", requester)
	return true
}

// SwarmJoinViaDelegate asks the swarm join delegate and returns its answer. The
// delegate may hold the question open while it asks the user; the wait ends
// when the delegate answers or when ctx ends, whichever comes first.
//
// why ctx closes the channel: Receive does not watch a context, and the op's
// context is detached from the requester's, so without this a delegate that
// never answers would hold the op forever. OpRequestMembership cancels ctx when
// the requesting node leaves.
//
// why a failure refuses: once joining is delegated, a delegate that cannot be
// reached has decided nothing, and falling back to accept-all would let a
// stopped delegate open the swarm to every node.
func (mod *Module) SwarmJoinViaDelegate(ctx *astral.Context, requester *astral.Identity) bool {
	delegate := mod.config.SwarmJoinDelegate.Get()
	if delegate == nil || delegate.IsZero() {
		return false
	}

	ch, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), delegate, user.OpDecideSwarmJoin, nil))
	if err != nil {
		mod.log.Errorv(1, "swarm join delegate %v: %v", delegate, err)
		return false
	}
	defer ch.Close()

	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()

	if err = ch.Send(&user.SwarmJoinRequest{Requester: requester}); err != nil {
		mod.log.Errorv(1, "swarm join delegate %v: %v", delegate, err)
		return false
	}

	obj, err := ch.Receive()
	if err != nil {
		if ctx.Err() != nil {
			mod.log.Logv(1, "swarm join delegate %v: requester left before a decision", delegate)
			return false
		}
		mod.log.Errorv(1, "swarm join delegate %v: %v", delegate, err)
		return false
	}

	// why: any other object is an answer this code cannot read, and an
	// unreadable answer must not read as permission.
	dec, ok := obj.(*user.SwarmJoinDecision)
	if !ok {
		mod.log.Errorv(1, "swarm join delegate %v answered %v", delegate, obj.ObjectType())
		return false
	}

	mod.log.Logv(1, "swarm join delegate %v decided allow=%v for %v", delegate, bool(dec.Allow), requester)

	return bool(dec.Allow)
}

// GetSwarmInvitePolicy returns the swarm invite delegate's policy when one is
// set, accept-all otherwise. It is read on every invitation.
func (mod *Module) GetSwarmInvitePolicy() user.SwarmInvitePolicy {
	if d := mod.config.SwarmInviteDelegate.Get(); d != nil && !d.IsZero() {
		return mod.SwarmInviteViaDelegate
	}
	return mod.SwarmInviteAcceptAll
}

var _ user.SwarmInvitePolicy = (*Module)(nil).SwarmInviteAcceptAll
var _ user.SwarmInvitePolicy = (*Module)(nil).SwarmInviteViaDelegate

func (mod *Module) SwarmInviteAcceptAll(_ *astral.Context, inviter *astral.Identity, contract *auth.Contract) bool {
	mod.log.Info("Accepting invitation from %v for %v join swarm till %v", inviter, contract.Subject, contract.ExpiresAt)
	return true
}

// SwarmInviteViaDelegate asks the swarm invite delegate and returns its answer.
// The wait ends when the delegate answers or when ctx ends, for the reason
// SwarmJoinViaDelegate gives; OpAcceptMembership cancels ctx when the inviter
// leaves.
//
// why a failure refuses: the same as SwarmJoinViaDelegate's — an unreachable
// delegate has decided nothing.
func (mod *Module) SwarmInviteViaDelegate(ctx *astral.Context, inviter *astral.Identity, contract *auth.Contract) bool {
	delegate := mod.config.SwarmInviteDelegate.Get()
	if delegate == nil || delegate.IsZero() {
		return false
	}

	ch, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), delegate, user.OpDecideSwarmInvite, nil))
	if err != nil {
		mod.log.Errorv(1, "swarm invite delegate %v: %v", delegate, err)
		return false
	}
	defer ch.Close()

	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()

	if err = ch.Send(&user.SwarmInviteRequest{Inviter: inviter, Contract: contract}); err != nil {
		mod.log.Errorv(1, "swarm invite delegate %v: %v", delegate, err)
		return false
	}

	obj, err := ch.Receive()
	if err != nil {
		if ctx.Err() != nil {
			mod.log.Logv(1, "swarm invite delegate %v: requester left before a decision", delegate)
			return false
		}
		mod.log.Errorv(1, "swarm invite delegate %v: %v", delegate, err)
		return false
	}

	// why: any other object is an answer this code cannot read, and an
	// unreadable answer must not read as permission.
	dec, ok := obj.(*user.SwarmInviteDecision)
	if !ok {
		mod.log.Errorv(1, "swarm invite delegate %v answered %v", delegate, obj.ObjectType())
		return false
	}

	mod.log.Logv(1, "swarm invite delegate %v decided allow=%v for invitation from %v", delegate, bool(dec.Allow), inviter)

	return bool(dec.Allow)
}
