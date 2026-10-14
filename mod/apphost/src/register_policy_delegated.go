package apphost

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// AppRegisterPolicyDelegated returns the policy that asks d. The node queries
// d's identity at d's operation, sends the request, and returns the one
// decision it reads back. The returned policy holds d, so a later change of
// delegate does not move a question already asked.
//
// The delegate may hold the question open while it asks the user; the wait
// ends when the delegate answers or when ctx ends.
//
// why ctx closes the channel: Receive does not watch a context, so a delegate
// that never answers would otherwise hold the op forever.
//
// why a failure is an error and not accept-all: once registration is
// delegated, a delegate that cannot be reached has decided nothing, and
// falling back would let a stopped delegate open the node to every app.
func (mod *Module) AppRegisterPolicyDelegated(d *policy.PolicyDelegate) apphostmod.AppRegisterPolicy {
	return func(ctx *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
		ch, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), d.Identity, string(d.Operation), nil))
		if err != nil {
			return nil, fmt.Errorf("app register delegate %v: %w", d.Identity, err)
		}
		defer ch.Close()

		stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
		defer stop()

		if err = ch.Send(req); err != nil {
			return nil, fmt.Errorf("app register delegate %v: %w", d.Identity, err)
		}

		obj, err := ch.Receive()
		if err != nil {
			return nil, fmt.Errorf("app register delegate %v: %w", d.Identity, err)
		}

		// why: any other object is an answer this code cannot read, and an
		// unreadable answer must not read as permission.
		dec, ok := obj.(*apphost.AppRegisterDecision)
		if !ok {
			return nil, errors.New("app register delegate answered " + obj.ObjectType())
		}

		return dec, nil
	}
}
