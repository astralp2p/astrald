package apphost

import (
	"context"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/apphost"
)

// GetAppRegisterPolicy returns the policy apphost.register applies: the app
// register delegate when one is set, accept-all otherwise. It is read on every
// registration, so setting or clearing the delegate takes effect on the next.
func (mod *Module) GetAppRegisterPolicy() apphost.AppRegisterPolicy {
	if mod.appRegisterDelegated() {
		return mod.AppRegisterViaDelegate
	}
	return mod.AppRegisterAcceptAll
}

// appRegisterDelegated reports whether app registration is decided by a
// delegate, which may wait on the user.
func (mod *Module) appRegisterDelegated() bool {
	d := mod.policy.AppRegisterDelegate.Get()
	return d != nil && !d.IsZero()
}

var _ apphost.AppRegisterPolicy = (*Module)(nil).AppRegisterAcceptAll
var _ apphost.AppRegisterPolicy = (*Module)(nil).AppRegisterViaDelegate

// AppRegisterAcceptAll admits every registration and writes every permit put in
// front of it onto the rail it was asked for, whether the caller's origin
// entitled it or the app simply asked. It is the permissive default its name
// claims to be: a node that cares which apps hold what installs a policy that
// decides.
func (mod *Module) AppRegisterAcceptAll(
	_ *astral.Context,
	origin string,
	_ string,
	_ *astral.Identity,
	_ bool,
	requestedGrantPermits, requestedContractPermits []*auth.Permit,
) apphost.AppRegisterOutcome {
	mod.log.Info("accepting registration from origin %v with %v grant permits and %v contract permits",
		origin, len(requestedGrantPermits), len(requestedContractPermits))

	return apphost.AppRegisterOutcome{
		GrantPermits:    requestedGrantPermits,
		ContractPermits: requestedContractPermits,
		Allow:           true,
	}
}

// AppRegisterViaDelegate asks the app register delegate and applies its answer.
// The delegate may hold the question open while it asks the user; the wait ends
// when the delegate answers or when ctx ends, whichever comes first.
//
// why ctx closes the channel: Receive does not watch a context, and the op's
// context is detached from the requester's, so without this a delegate that
// never answers would hold the op forever. OpRegister cancels ctx when the
// registering app leaves.
//
// why a failure refuses: once registration is delegated, a delegate that cannot
// be reached has decided nothing, and falling back to accept-all would let a
// stopped delegate open the node to every app.
func (mod *Module) AppRegisterViaDelegate(
	ctx *astral.Context,
	origin string,
	name string,
	caller *astral.Identity,
	anonymous bool,
	requestedGrantPermits, requestedContractPermits []*auth.Permit,
) apphost.AppRegisterOutcome {
	delegate := mod.policy.AppRegisterDelegate.Get()
	if delegate == nil || delegate.IsZero() {
		return apphost.AppRegisterOutcome{}
	}

	if ctx.Err() != nil {
		mod.log.Logv(1, "app register delegate %v: requester left before a decision", delegate)
		return apphost.AppRegisterOutcome{}
	}

	ch, err := query.Route(ctx, mod.node, query.New(mod.node.Identity(), delegate, apphost.OpDecideAppRegister, nil))
	if err != nil {
		mod.logDelegateFailure(ctx, delegate, err)
		return apphost.AppRegisterOutcome{}
	}
	defer ch.Close()

	stop := context.AfterFunc(ctx, func() { _ = ch.Close() })
	defer stop()

	err = ch.Send(&apphost.AppRegisterRequest{
		Origin:          astral.String8(origin),
		GrantPermits:    requestedGrantPermits,
		ContractPermits: requestedContractPermits,
		Caller:          caller,
		Anonymous:       astral.Bool(anonymous),
		Name:            astral.String8(name),
	})
	if err != nil {
		mod.logDelegateFailure(ctx, delegate, err)
		return apphost.AppRegisterOutcome{}
	}

	obj, err := ch.Receive()
	if err != nil {
		mod.logDelegateFailure(ctx, delegate, err)
		return apphost.AppRegisterOutcome{}
	}

	// why: any other object is an answer this code cannot read, and an
	// unreadable answer must not read as permission.
	dec, ok := obj.(*apphost.AppRegisterDecision)
	if !ok {
		mod.log.Errorv(1, "app register delegate %v answered %v", delegate, obj.ObjectType())
		return apphost.AppRegisterOutcome{}
	}

	mod.log.Logv(1, "app register delegate %v decided allow=%v with %v grant permits, %v contract permits and %v evaluators",
		delegate, bool(dec.Allow), len(dec.GrantPermits), len(dec.ContractPermits), len(dec.Evaluators))

	return apphost.AppRegisterOutcome{
		GrantPermits:    dec.GrantPermits,
		ContractPermits: dec.ContractPermits,
		Evaluators:      dec.Evaluators,
		Allow:           bool(dec.Allow),
	}
}

// logDelegateFailure logs why a question to the app register delegate went
// unanswered: the requester leaving is expected and logged as such, anything
// else is an error.
func (mod *Module) logDelegateFailure(ctx *astral.Context, delegate *astral.Identity, err error) {
	if ctx.Err() != nil {
		mod.log.Logv(1, "app register delegate %v: requester left before a decision", delegate)
		return
	}
	mod.log.Errorv(1, "app register delegate %v: %v", delegate, err)
}
