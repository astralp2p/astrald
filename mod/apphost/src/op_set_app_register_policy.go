package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAppRegisterPolicyArgs struct {
	// Identity and Operation name the delegate that decides apphost.register.
	// Both empty restores AcceptAll; one alone is an error.
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
func (mod *Module) OpSetAppRegisterPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetAppRegisterPolicyArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	if args.Identity == "" && args.Operation == "" {
		if err := mod.policy.AppRegister.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "app register policy restored to accept-all")
		return ch.Send(&astral.Ack{})
	}

	d, err := mod.newDelegate(args.Identity, args.Operation, args.Timeout)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if err = mod.policy.AppRegister.Set(ctx, d); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "app register policy delegated to %v:%v", d.Identity, d.Operation)
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
