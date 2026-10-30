package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetPolicyArgs struct {
	// Identity and Operation name the delegate the op's policy asks. Both
	// empty restores the bundled policy; one alone is an error.
	Identity  string
	Operation string
	// Timeout bounds one question. Zero waits policy.MaxTimeout; above it is
	// refused.
	Timeout astral.Duration
	In      string
	Out     string
}

// OpSetAppRegisterPolicy selects the app registration policy and answers Ack
// once the choice is persisted.
//
// why AdminManageApps: the delegate decides which permits every new app
// receives, which is the administration of app credentials that action covers.
func (mod *Module) OpSetAppRegisterPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetPolicyArgs) error {
	return mod.setPolicyDelegate(ctx, q, args, &mod.policy.AppRegister)
}

// OpSetGrantRequestPolicy selects the policy for apphost.request_grant and
// answers Ack once the choice is persisted.
//
// why AdminManageApps: the delegate decides which permits apps may add to
// themselves, which is the administration of app credentials that action
// covers.
func (mod *Module) OpSetGrantRequestPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetPolicyArgs) error {
	return mod.setPolicyDelegate(ctx, q, args, &mod.policy.GrantRequest)
}

// setPolicyDelegate admits a caller holding AdminManageApps, then writes the
// delegate args name into v, or clears v when args name none.
func (mod *Module) setPolicyDelegate(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opSetPolicyArgs,
	v *tree.Value[*policy.Delegate],
) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if args.Identity == "" && args.Operation == "" {
		if err := v.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "%v restored to the bundled policy", q.QueryString())
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

// newDelegate resolves name and builds the delegate the setter stores.
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
