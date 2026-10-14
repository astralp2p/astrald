package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetSwarmPolicyArgs struct {
	// Identity is the identity or alias that decides.
	Identity string
	// Operation is the op the identity serves for the decision.
	Operation string
	In        string
	Out       string
}

// OpSetSwarmJoinPolicy selects the policy for unsolicited join requests.
// Identity and operation together delegate the decision; neither restores
// accept-all; only one is an error. It answers Ack once the choice is persisted.
func (mod *Module) OpSetSwarmJoinPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmPolicyArgs) error {
	return mod.setSwarmPolicy(ctx, q, args, mod.SetSwarmJoinRequestPolicy)
}

// OpSetSwarmInvitePolicy selects the policy for incoming invitations. Identity
// and operation together delegate the decision; neither restores accept-all;
// only one is an error. It answers Ack once the choice is persisted.
func (mod *Module) OpSetSwarmInvitePolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmPolicyArgs) error {
	return mod.setSwarmPolicy(ctx, q, args, mod.SetSwarmInvitePolicy)
}

// setSwarmPolicy admits a local caller holding AdminManageApps and applies set.
//
// why AdminManageApps and not AdminSwarm: AdminSwarm is held by the user of the
// active contract, and a node with none — the node an invite policy matters
// for — would have no one able to set it. AdminManageApps is what every policy
// delegate setter requires.
func (mod *Module) setSwarmPolicy(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opSetSwarmPolicyArgs,
	set func(*astral.Context, *policy.PolicyDelegate) error,
) error {
	// why local only: the policy is this node's setting, so a caller off a link
	// has no standing to write it.
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	d := &policy.PolicyDelegate{}

	switch {
	case args.Identity == "" && args.Operation == "":
	case args.Identity == "" || args.Operation == "":
		return ch.Send(astral.NewError("identity and operation are set together"))
	default:
		id, err := mod.Dir.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
		d.Identity = id
		d.Operation = astral.String8(args.Operation)
	}

	if err := set(ctx, d); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "%v set to %v:%v", q.QueryString, d.Identity, d.Operation)

	return ch.Send(&astral.Ack{})
}
