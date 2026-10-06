package user

import (
	"github.com/astralp2p/astral-go/api/auth"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetSwarmInviteDelegateArgs struct {
	// Delegate is the identity or alias that decides swarm invites. Empty
	// clears the delegate and restores accept-all.
	Delegate string
	In       string
	Out      string
}

// OpSetSwarmInviteDelegate sets or clears the identity that decides swarm invites.
//
// why ConfigureNodeState and not AdminSwarm: an invitation is decided on a node
// no user has claimed yet, where AdminSwarm has no issuer to allow. The delegate
// is a tree value, and ConfigureNodeState is what a tree write already requires.
func (mod *Module) OpSetSwarmInviteDelegate(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmInviteDelegateArgs) error {
	// why: the delegate is this node's setting, so a caller off a link has no
	// standing to write it.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Delegate == "" {
		if err := mod.config.SwarmInviteDelegate.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "swarm invite delegate cleared")
		return ch.Send(&astral.Ack{})
	}

	delegate, err := mod.Dir.ResolveIdentity(args.Delegate)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if delegate.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if err = mod.config.SwarmInviteDelegate.Set(ctx, delegate); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "swarm invite delegate set to %v", delegate)

	return ch.Send(&astral.Ack{})
}
