package apphost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// delegateTimeout bounds one question to a registration delegate.
//
// why 60 s: the policy runs while apphost.register is still en route, and the
// router gives up on an en-route query after 60 s, so a later answer reaches
// nobody.
const delegateTimeout = 60 * time.Second

// AppRegisterPolicyDelegated returns the policy that asks d. The node queries
// d's identity at d's operation, sends the request, and reads one decision
// back. The returned policy holds d, so a later change of delegate does not
// move a question already asked.
//
// The delegate may hold the question open while it asks the user. The wait
// ends when the delegate answers, when ctx ends, or after delegateTimeout.
//
// why a failure refuses and not accepts: once registration is delegated, a
// delegate that cannot be reached has decided nothing, and falling back would
// let a stopped delegate open the node to every app.
func (mod *Module) AppRegisterPolicyDelegated(d *policy.PolicyDelegate) apphostmod.AppRegisterPolicy {
	return func(
		ctx *astral.Context,
		origin, name string,
		requestedGrantPermits, requestedContractPermits []*auth.Permit,
	) ([]*auth.Permit, []*auth.Permit, bool) {
		dec, err := mod.askRegisterDelegate(ctx, d, &apphost.AppRegisterRequest{
			Origin:          astral.String8(origin),
			Name:            astral.String8(name),
			GrantPermits:    requestedGrantPermits,
			ContractPermits: requestedContractPermits,
		})
		if err != nil {
			mod.log.Logv(1, "registration refused: %v", err)
			return nil, nil, false
		}

		return dec.GrantPermits, dec.ContractPermits, bool(dec.Allow)
	}
}

// askRegisterDelegate sends req to d and returns the decision it reads back.
//
// why ctx closes the channel: Receive does not watch a context, so a delegate
// that never answers would otherwise hold the op forever.
func (mod *Module) askRegisterDelegate(ctx *astral.Context, d *policy.PolicyDelegate, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
	ctx, cancel := ctx.WithTimeout(delegateTimeout)
	defer cancel()

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
