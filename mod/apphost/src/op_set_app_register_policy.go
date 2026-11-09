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
	// empty restores the config value; one alone is an error.
	Identity  string
	Operation string
	// Timeout bounds one question. Zero waits policy.MaxTimeout; above it is
	// refused.
	Timeout astral.Duration
	In      string
	Out     string
}

// opSetGrantRequestPolicyArgs selects the grant request policy: Mode alone
// selects accept_all or reject_all; Identity and Operation, with an optional
// Timeout, select the delegate mode; all empty clears both, and the unset
// policy refuses every request.
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
// why ConfigureNodeState: the delegate is a tree value; see maySetPolicies.
func (mod *Module) OpSetAppRegisterPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetPolicyArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.maySetPolicies(ctx, q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: mode belongs to apphost.set_grant_request_policy; without this a
	// caller naming it here would clear the register delegate.
	if err := refuseRemovedArgs(q.QueryString(), "mode"); err != nil {
		return ch.Send(astral.Err(err))
	}

	d, err := mod.newDelegate(args.Identity, args.Operation, args.Timeout)
	if err == nil {
		err = writePolicyDelegate(ctx, d, &mod.policy.AppRegister)
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "%v persisted", q.QueryString())
	return ch.Send(&astral.Ack{})
}

// OpSetGrantRequestPolicy selects the policy for apphost.request_grant and
// answers Ack once the choice is persisted. A mode clears the delegate, and a
// delegate clears the mode. A mode given with a timeout is refused.
//
// why ConfigureNodeState: the policy is a tree value; see maySetPolicies.
func (mod *Module) OpSetGrantRequestPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetGrantRequestPolicyArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.maySetPolicies(ctx, q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	var err error
	switch args.Mode {
	case "":
		var d *policy.Delegate
		d, err = mod.newDelegate(args.Identity, args.Operation, args.Timeout)
		if err == nil {
			err = writePolicyDelegate(ctx, d, &mod.policy.GrantRequest)
		}
		if err == nil {
			err = mod.policy.GrantRequestMode.Clear(ctx)
		}
	case apphostmod.GrantRequestAcceptAll, apphostmod.GrantRequestRejectAll:
		if args.Identity != "" || args.Operation != "" {
			return ch.Send(astral.NewError("give mode or a delegate, not both"))
		}
		// why: timeout bounds a delegate's question, and a mode asks no one.
		if err = refuseRemovedArgs(q.QueryString(), "timeout"); err != nil {
			return ch.Send(astral.Err(err))
		}
		// why the delegate first: a delegate outranks the mode, so a failed
		// mode write leaves no delegate in force, only the earlier mode.
		err = mod.policy.GrantRequest.Clear(ctx)
		if err == nil {
			err = mod.policy.GrantRequestMode.Set(ctx, astral.NewString8(args.Mode))
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

// writePolicyDelegate writes d into v, or clears v when d is nil.
func writePolicyDelegate(ctx *astral.Context, d *policy.Delegate, v *tree.Value[*policy.Delegate]) error {
	if d == nil {
		return v.Clear(ctx)
	}
	return v.Set(ctx, d)
}

// newDelegate resolves name and builds the delegate the setter stores. Both
// name and op empty return nil, which restores the config value.
//
// why not the node itself: the admitting op would ask itself and recurse.
func (mod *Module) newDelegate(name, op string, timeout astral.Duration) (*policy.Delegate, error) {
	if name == "" && op == "" {
		return nil, nil
	}

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
