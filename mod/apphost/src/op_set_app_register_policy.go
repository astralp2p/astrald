package apphost

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
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

// opSetGrantRequestPolicyArgs selects the grant request policy: Mode alone
// selects accept_all or reject_all; Identity and Operation select the delegate
// mode; all empty restores the bundled reject_all.
type opSetGrantRequestPolicyArgs struct {
	Mode      string
	Identity  string
	Operation string
	Timeout   astral.Duration
	In        string
	Out       string
}

// OpSetAppRegisterPolicy selects the app registration policy and answers Ack
// once the choice is persisted.
//
// why AdminManageApps: the delegate decides which permits every new app
// receives, which is the administration of app credentials that action covers.
func (mod *Module) OpSetAppRegisterPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetPolicyArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: mode belongs to apphost.set_grant_request_policy; without this a
	// caller naming it here would clear the register delegate.
	if err := refuseRemovedArgs(q.QueryString(), "mode"); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.writePolicyDelegate(ctx, args.Identity, args.Operation, args.Timeout, &mod.policy.AppRegister); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "%v persisted", q.QueryString())
	return ch.Send(&astral.Ack{})
}

// OpSetGrantRequestPolicy selects the policy for apphost.request_grant and
// answers Ack once the choice is persisted. A mode clears the delegate, and a
// delegate clears the mode.
//
// why AdminManageApps: the policy decides which permits apps may add to
// themselves, which is the administration of app credentials that action
// covers.
func (mod *Module) OpSetGrantRequestPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetGrantRequestPolicyArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	var err error
	switch args.Mode {
	case "":
		err = mod.writePolicyDelegate(ctx, args.Identity, args.Operation, args.Timeout, &mod.policy.GrantRequest)
		if err == nil {
			err = mod.policy.GrantRequestMode.Clear(ctx)
		}
	case apphostmod.GrantRequestAcceptAll, apphostmod.GrantRequestRejectAll:
		if args.Identity != "" || args.Operation != "" {
			return ch.Send(astral.NewError("give mode or a delegate, not both"))
		}
		err = mod.policy.GrantRequestMode.Set(ctx, astral.NewString8(args.Mode))
		if err == nil {
			err = mod.policy.GrantRequest.Clear(ctx)
		}
	default:
		return ch.Send(astral.NewError("unknown mode: " + args.Mode))
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "%v persisted", q.QueryString())
	return ch.Send(&astral.Ack{})
}

// writePolicyDelegate writes the delegate identity and op name into v, or
// clears v when both are empty.
func (mod *Module) writePolicyDelegate(
	ctx *astral.Context,
	identity, op string,
	timeout astral.Duration,
	v *tree.Value[*policy.Delegate],
) error {
	if identity == "" && op == "" {
		return v.Clear(ctx)
	}

	d, err := mod.newDelegate(identity, op, timeout)
	if err != nil {
		return err
	}
	return v.Set(ctx, d)
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
