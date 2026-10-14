package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// SwarmJoinRequestPolicyDelegated returns the join policy that asks d. The
// returned policy holds d, so a later change of delegate does not move a
// question already asked.
func (mod *Module) SwarmJoinRequestPolicyDelegated(d *policy.PolicyDelegate) usermod.SwarmJoinRequestPolicy {
	return func(ctx *astral.Context, req *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error) {
		obj, err := mod.askDelegate(ctx, d, req)
		if err != nil {
			return nil, err
		}

		dec, ok := obj.(*user.SwarmJoinDecision)
		if !ok {
			return nil, errors.New("swarm join delegate answered " + obj.ObjectType())
		}
		return dec, nil
	}
}

// SwarmInvitePolicyDelegated returns the invite policy that asks d. The
// returned policy holds d, so a later change of delegate does not move a
// question already asked.
func (mod *Module) SwarmInvitePolicyDelegated(d *policy.PolicyDelegate) usermod.SwarmInvitePolicy {
	return func(ctx *astral.Context, req *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error) {
		obj, err := mod.askDelegate(ctx, d, req)
		if err != nil {
			return nil, err
		}

		dec, ok := obj.(*user.SwarmInviteDecision)
		if !ok {
			return nil, errors.New("swarm invite delegate answered " + obj.ObjectType())
		}
		return dec, nil
	}
}

// askDelegate queries d's identity at d's operation as the node, sends req, and
// returns the one object it reads back. The wait ends when the delegate answers
// or when ctx ends.
//
// why ctx closes the channel: Receive does not watch a context, so a delegate
// that never answers would otherwise hold the op forever.
//
// why a failure is an error and not accept-all: a delegate that cannot be
// reached has decided nothing.
func (mod *Module) askDelegate(ctx *astral.Context, d *policy.PolicyDelegate, req astral.Object) (astral.Object, error) {
	ch, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), d.Identity, string(d.Operation), nil))
	if err != nil {
		return nil, fmt.Errorf("delegate %v: %w", d.Identity, err)
	}
	defer ch.Close()

	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()

	if err = ch.Send(req); err != nil {
		return nil, fmt.Errorf("delegate %v: %w", d.Identity, err)
	}

	obj, err := ch.Receive()
	if err != nil {
		return nil, fmt.Errorf("delegate %v: %w", d.Identity, err)
	}

	return obj, nil
}

// saveDelegate persists d in v, clearing v for a zero delegate.
func saveDelegate(ctx *astral.Context, v *tree.Value[*policy.PolicyDelegate], d *policy.PolicyDelegate) error {
	if d.IsZero() {
		return v.Clear(ctx)
	}
	return v.Set(ctx, d)
}
