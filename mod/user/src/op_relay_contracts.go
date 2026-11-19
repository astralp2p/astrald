package user

import (
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRelayContractsArgs struct {
	Issuer *astral.Identity `query:"required"`
	Out    string
}

// OpRelayContracts streams the relay contracts this node holds for args.Issuer
// to another member of the local swarm, terminated by EOS. A caller that is not
// such a member is refused with auth.ErrNotPermitted.
// Spec: .ai/system/protocols/user/ops/user.relay_contracts.md
//
// why: no named action guards this op; a member asks it as itself on a routing
// miss (FetchRelayContracts), and swarm membership is the guard.
func (mod *Module) OpRelayContracts(ctx *astral.Context, q *routing.IncomingQuery, args opRelayContractsArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Caller().IsEqual(mod.node.Identity()) || !slices.ContainsFunc(mod.LocalSwarm(), q.Caller().IsEqual) {
		return ch.Send(auth.ErrNotPermitted)
	}

	contracts, err := mod.Auth.SignedContracts().
		WithIssuer(args.Issuer).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx.ExcludeZone(astral.ZoneNetwork))
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	for _, sc := range contracts {
		// note: the rule the first-link sync forwards by; a contract whose subject
		// is the caller is the caller's own.
		if !mod.isForwardable(sc, q.Caller()) {
			continue
		}
		if err = ch.Send(sc); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
