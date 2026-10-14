package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// delegateTimeout bounds one question to a swarm policy delegate.
//
// why: the op runs on a context detached from the requester's, so without a
// bound a delegate that never answers holds the op forever.
const delegateTimeout = 60 * time.Second

// SwarmJoinRequestPolicyDelegated returns the join policy that asks d. The
// returned policy holds d, so a later change of delegate does not move a
// question already asked. A failure declines.
func (mod *Module) SwarmJoinRequestPolicyDelegated(d *policy.PolicyDelegate) usermod.SwarmJoinRequestPolicy {
	return func(ctx *astral.Context, requester *astral.Identity) bool {
		obj, err := mod.askDelegate(ctx, d, &user.SwarmJoinRequest{Requester: requester})
		if err != nil {
			mod.log.Logv(1, "join request from %v refused: %v", requester, err)
			return false
		}

		dec, ok := obj.(*user.SwarmJoinDecision)
		if !ok {
			mod.log.Logv(1, "join request from %v refused: delegate answered %v", requester, obj.ObjectType())
			return false
		}
		return bool(dec.Allow)
	}
}

// SwarmInvitePolicyDelegated returns the invite policy that asks d. The
// returned policy holds d, so a later change of delegate does not move a
// question already asked. A failure declines.
func (mod *Module) SwarmInvitePolicyDelegated(d *policy.PolicyDelegate) usermod.SwarmInvitePolicy {
	return func(ctx *astral.Context, inviter *astral.Identity, contract *auth.Contract) bool {
		obj, err := mod.askDelegate(ctx, d, &user.SwarmInviteRequest{Inviter: inviter, Contract: contract})
		if err != nil {
			mod.log.Logv(1, "invitation from %v refused: %v", inviter, err)
			return false
		}

		dec, ok := obj.(*user.SwarmInviteDecision)
		if !ok {
			mod.log.Logv(1, "invitation from %v refused: delegate answered %v", inviter, obj.ObjectType())
			return false
		}
		return bool(dec.Allow)
	}
}

// askDelegate queries d's identity at d's operation as the node, sends req, and
// returns the one object it reads back. The wait ends when the delegate
// answers, when ctx ends, or after delegateTimeout.
//
// why ctx closes the channel: Receive does not watch a context, so a delegate
// that never answers would otherwise hold the op forever.
//
// why a failure is an error and not accept-all: a delegate that cannot be
// reached has decided nothing.
func (mod *Module) askDelegate(ctx *astral.Context, d *policy.PolicyDelegate, req astral.Object) (astral.Object, error) {
	ctx, cancel := ctx.WithTimeout(delegateTimeout)
	defer cancel()

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
	if obj == nil {
		return nil, errors.New("delegate answered nothing")
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
