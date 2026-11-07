package sys

import (
	"fmt"
	"slices"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	sysapi "github.com/astralp2p/astral-go/api/sys"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterManagerArgs struct {
	In  string
	Out string
}

// OpRegisterManager provisions a brand-new App identity, as apphost.register
// does, and appoints it the node's manager. The op reads one
// mod.sys.register_manager_request after accept. Each policy binding of the
// request is stored as a delegate whose identity is the new manager; every
// policy the request does not name loses its stored delegate. The node writes
// the manager record last and answers the access token.
//
// A node that is not fresh answers sysapi.ErrNotFresh. The app register
// policy is not asked.
// Spec: .ai/system/protocols/sys/ops/sys.register_manager.md
//
// why the record last: a failure before it leaves the node fresh, and a retry
// replaces the delegates the failed attempt wrote.
// why: no named action guards this op, by decision (2026-11-06); only a fresh
// node answers, and its first caller becomes the manager with the permits it
// asks for.
func (mod *Module) OpRegisterManager(ctx *astral.Context, query *routing.IncomingQuery, args opRegisterManagerArgs) error {
	ch := channel.New(query.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: identities minted over a link are indistinguishable from local apps.
	if query.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(query.Origin()))
	}

	var req *sysapi.RegisterManagerRequest
	if err := ch.Switch(channel.Expect(&req), channel.FailOnEOF()); err != nil {
		return ch.Send(astral.Err(err))
	}
	if err := mod.validateManagerRequest(req); err != nil {
		return ch.Send(astral.Err(err))
	}

	token, err := mod.appointManager(ctx, req)
	if err != nil {
		mod.log.Logv(1, "manager appointment refused: %v", err)
		return ch.Send(astral.Err(err))
	}

	mod.log.Log("appointed manager %v", token.Identity)
	return ch.Send(token)
}

// appointManager provisions the manager, binds its policies and records the
// appointment while no other appointment runs.
func (mod *Module) appointManager(ctx *astral.Context, req *sysapi.RegisterManagerRequest) (*apphostapi.AccessToken, error) {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if mod.NodeState(ctx) != user.StateFresh {
		return nil, sysapi.ErrNotFresh
	}

	token, err := mod.apps.ProvisionApp(ctx, req.GrantPermits, req.ContractPermits)
	if err != nil {
		return nil, err
	}

	if err = mod.bindManagerPolicies(ctx, token.Identity, req.Policies); err != nil {
		return nil, err
	}

	if err = mod.appointment.Manager.Set(ctx, token.Identity); err != nil {
		return nil, err
	}
	return token, nil
}

// validateManagerRequest refuses an invalid permit, an unknown or repeated
// policy name, a binding without an operation, a timeout above
// policy.MaxTimeout, and a user policy on a node without the user module.
func (mod *Module) validateManagerRequest(req *sysapi.RegisterManagerRequest) error {
	for _, p := range slices.Concat(req.GrantPermits, req.ContractPermits) {
		if err := auth.ValidatePermit(p); err != nil {
			return err
		}
	}

	var seen []string
	for _, b := range req.Policies {
		name := b.Policy.String()
		switch {
		case !slices.Contains(sysapi.PolicyNames, name):
			return fmt.Errorf("unknown policy: %v", name)
		case slices.Contains(seen, name):
			return fmt.Errorf("policy bound twice: %v", name)
		case b.Operation == "":
			return fmt.Errorf("policy %v: no operation named", name)
		case b.Timeout < 0 || b.Timeout.Duration() > policy.MaxTimeout:
			return fmt.Errorf("policy %v: timeout outside 0..%v", name, policy.MaxTimeout)
		case mod.User == nil && !isApphostPolicy(name):
			return fmt.Errorf("policy %v: the user module is not loaded", name)
		}
		seen = append(seen, name)
	}
	return nil
}

// bindManagerPolicies stores, for every policy name, the delegate bindings
// name with manager as identity, and clears the delegate of every name
// bindings omit.
func (mod *Module) bindManagerPolicies(ctx *astral.Context, manager *astral.Identity, bindings []*sysapi.PolicyBinding) error {
	for _, name := range sysapi.PolicyNames {
		var d *policy.Delegate
		for _, b := range bindings {
			if b.Policy.String() == name {
				d = &policy.Delegate{Identity: manager, Operation: b.Operation, Timeout: b.Timeout}
			}
		}
		if err := mod.setPolicyDelegate(ctx, name, d); err != nil {
			return fmt.Errorf("policy %v: %w", name, err)
		}
	}
	return nil
}

// setPolicyDelegate stores d as the delegate of the named policy in the module
// that owns it, or clears it when d is nil. A user policy on a node without
// the user module is skipped when d is nil.
func (mod *Module) setPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error {
	store := mod.apphostPolicies
	if !isApphostPolicy(name) {
		if mod.User == nil {
			if d == nil {
				return nil
			}
			return fmt.Errorf("the user module is not loaded")
		}
		store = mod.User
	}
	return store.SetPolicyDelegate(ctx, name, d)
}

// isApphostPolicy reports whether apphost stores the delegate of name.
func isApphostPolicy(name string) bool {
	return name == sysapi.PolicyAppRegister || name == sysapi.PolicyGrantRequest
}
