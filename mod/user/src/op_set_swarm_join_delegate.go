package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetSwarmJoinDelegateArgs struct {
	// Delegate is the identity or alias that decides swarm joins. Empty
	// clears the delegate and restores accept-all.
	Delegate string
	In       string
	Out      string
}

// OpSetSwarmJoinDelegate sets or clears the identity that decides swarm joins.
//
// why AdminSwarm: the delegate decides which nodes join the swarm, which is
// the authority user.adopt and user.expel already require.
func (mod *Module) OpSetSwarmJoinDelegate(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmJoinDelegateArgs) error {
	// why: the delegate is this node's setting, so a caller off a link has no
	// standing to write it.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Delegate == "" {
		if err := mod.config.SwarmJoinDelegate.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "swarm join delegate cleared")
		return ch.Send(&astral.Ack{})
	}

	delegate, err := mod.Dir.ResolveIdentity(args.Delegate)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if delegate.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if err = mod.config.SwarmJoinDelegate.Set(ctx, delegate); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "swarm join delegate set to %v", delegate)

	return ch.Send(&astral.Ack{})
}
