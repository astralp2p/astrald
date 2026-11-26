package nodes

import (
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opResolveEndpointsArgs struct {
	Identity string `query:"required"`
	Out      string
}

func (mod *Module) OpResolveEndpoints(ctx *astral.Context, q *routing.IncomingQuery, args opResolveEndpointsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	admin := mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())})
	// why the member check before the lookup: an outsider learns nothing, not
	// even whether the alias resolves.
	if !admin && !mod.isSwarmMember(q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: the zero identity names no node, and ResolveEndpoints fans the target
	// out to every registered resolver without testing it.
	targetID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if targetID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}
	if !admin && !mod.mayReadMemberEndpoints(q.Caller(), targetID) {
		return ch.Send(auth.ErrNotPermitted)
	}

	endpoints, err := mod.ResolveEndpoints(ctx.WithIdentity(q.Caller()), targetID)
	if err != nil {
		mod.log.Error("resolve endpoints: %v", err)
		return ch.Send(astral.Err(err))
	}

	for endpoint := range endpoints {
		err = ch.Send(endpoint)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}

// mayReadMemberEndpoints reports whether caller, a caller without
// AdminNetworkAction, may read target's endpoints: both are current members of
// this node's local swarm, or target is this node.
//
// why: endpoints travel only through nodes.resolve_endpoints, and every member
// links to every other member, so a Member that could not pull a member's
// endpoints would never link to another Member. The rows are private to the
// swarm, so neither party may be an outsider.
// why not AdminNetworkAction: a Member administers no node; this reads
// endpoint rows about members and grants no other network-admin op.
func (mod *Module) mayReadMemberEndpoints(caller, target *astral.Identity) bool {
	if !mod.isSwarmMember(caller) {
		return false
	}
	return target.IsEqual(mod.node.Identity()) || mod.isSwarmMember(target)
}

// isSwarmMember reports whether id is a current member of this node's local
// swarm other than this node itself.
// note: LocalSwarm leaves out expelled nodes.
func (mod *Module) isSwarmMember(id *astral.Identity) bool {
	if id.IsZero() || id.IsEqual(mod.node.Identity()) {
		return false
	}
	return slices.ContainsFunc(mod.User.LocalSwarm(), id.IsEqual)
}
