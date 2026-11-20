package user

import (
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// relayDigestOp is the name of the op OpRelayDigest serves.
// why local: the op is a hint between members of one swarm, not part of the
// astral-go client surface.
const relayDigestOp = "user.relay_digest"

type opRelayDigestArgs struct {
	Out string
}

// OpRelayDigest streams the object IDs of the relay contracts this node holds
// to another member of the local swarm, terminated by EOS. A caller that is not
// such a member is refused with auth.ErrNotPermitted.
// Spec: .ai/system/protocols/user/ops/user.relay_digest.md
//
// why: no named action guards this op; a member asks it as itself before a
// relay contract push (syncAppContracts), and swarm membership is the guard.
// why the every-permit filter and no forwarding filter: the digest lists the
// relay contracts this node holds, the ones where every permit is for
// relay_for (isRelayForContract, the rule of user.relay_contracts), whoever
// issued or hosts them. A contract that carries another permit beside
// relay_for is never listed, and isForwardable never pushes it. The digest
// leaves the choice of what to push to the pusher (isForwardable). A contract
// this node refused is absent from its index, so it is pushed again.
func (mod *Module) OpRelayDigest(ctx *astral.Context, q *routing.IncomingQuery, args opRelayDigestArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Caller().IsEqual(mod.node.Identity()) || !slices.ContainsFunc(mod.LocalSwarm(), q.Caller().IsEqual) {
		return ch.Send(auth.ErrNotPermitted)
	}

	contracts, err := mod.Auth.SignedContracts().
		WithAction(&nodes.RelayForAction{}).
		Find(ctx.ExcludeZone(astral.ZoneNetwork))
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	for _, sc := range contracts {
		if sc.IsNil() || !isRelayForContract(sc.Contract) {
			continue
		}
		id, err := astral.ResolveObjectID(sc)
		if err != nil {
			mod.log.Logv(1, "relay digest: resolving a contract id: %v", err)
			continue
		}
		if err = ch.Send(id); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
