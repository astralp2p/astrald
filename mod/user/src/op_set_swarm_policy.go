package user

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetSwarmPolicyArgs struct {
	// Identity and Operation name the delegate that decides. Both empty
	// clears the delegate, which restores the config value, or the bundled
	// accept-all for contract accept; one alone is an error.
	Identity  string
	Operation string
	// Timeout bounds one question. Zero waits policy.MaxTimeout; above it is
	// refused.
	Timeout astral.Duration
	In      string
	Out     string
}

// OpSetSwarmJoinPolicy selects the policy for unsolicited join requests and
// answers Ack once the choice is persisted.
func (mod *Module) OpSetSwarmJoinPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmPolicyArgs) error {
	return mod.setSwarmPolicy(ctx, q, args, &mod.policy.SwarmJoin)
}

// OpSetSwarmInvitePolicy selects the policy for incoming invitations and
// answers Ack once the choice is persisted.
func (mod *Module) OpSetSwarmInvitePolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmPolicyArgs) error {
	return mod.setSwarmPolicy(ctx, q, args, &mod.policy.SwarmInvite)
}

// OpSetContractAcceptPolicy selects the policy for contracts user.accept_contract
// activates and answers Ack once the choice is persisted.
func (mod *Module) OpSetContractAcceptPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetSwarmPolicyArgs) error {
	return mod.setSwarmPolicy(ctx, q, args, &mod.policy.ContractAccept)
}

// setSwarmPolicy admits a caller holding ConfigureNodeState, then writes the
// delegate args name into v, or clears v when args name none.
//
// why ConfigureNodeState and not AdminSwarm: AdminSwarm is held by the user of
// the active contract, and a node with none — the node an invite or contract
// accept policy matters for — would have no one able to set it.
// ConfigureNodeState is what every policy setter requires.
func (mod *Module) setSwarmPolicy(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opSetSwarmPolicyArgs,
	v *tree.Value[*policy.Delegate],
) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.maySetPolicies(ctx, q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if args.Identity == "" && args.Operation == "" {
		if err := v.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "%v delegate cleared", q.QueryString())
		return ch.Send(&astral.Ack{})
	}

	d, err := mod.newDelegate(args.Identity, args.Operation, args.Timeout)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if err = v.Set(ctx, d); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "%v delegated to %v:%v", q.QueryString(), d.Identity, d.Operation)
	return ch.Send(&astral.Ack{})
}

// newDelegate resolves name and builds the delegate a setter stores.
//
// why not the node itself: the admitting op would ask itself and recurse.
func (mod *Module) newDelegate(name, op string, timeout astral.Duration) (*policy.Delegate, error) {
	id, err := mod.Dir.ResolveIdentity(name)
	if err != nil {
		return nil, err
	}
	if id.IsEqual(mod.node.Identity()) {
		return nil, errors.New("the node cannot be its own delegate")
	}

	d := &policy.Delegate{Identity: id, Operation: astral.String8(op), Timeout: timeout}
	return d, d.Validate()
}

// maySetPolicies reports whether caller may select a policy: a holder of
// ConfigureNodeState. The manager holds it; see mod/sys.
//
// why ConfigureNodeState: a policy is a tree value, and that action covers
// changes to the tree.
func (mod *Module) maySetPolicies(ctx *astral.Context, caller *astral.Identity) bool {
	return mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(caller)})
}
