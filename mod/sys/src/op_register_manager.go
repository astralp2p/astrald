package sys

import (
	"errors"
	"slices"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
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
// mod.sys.register_manager_request after accept. The manager holds a
// node→manager contract of the request's GrantPermits and ContractPermits,
// which the fresh node signs as its own root. A request that binds a policy
// is refused: the five policies are requests in SwarmState, answered with
// user.answer_request. The node writes the manager record last and answers the
// access token.
//
// A node that is not fresh answers sysapi.ErrNotFresh.
// Spec: .ai/system/protocols/sys/ops/sys.register_manager.md
//
// why the record last: a failure before it leaves the node fresh.
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

// appointManager provisions the manager and records the appointment while no other appointment runs.
func (mod *Module) appointManager(ctx *astral.Context, req *sysapi.RegisterManagerRequest) (*apphostapi.AccessToken, error) {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if mod.NodeState(ctx) != user.StateFresh {
		return nil, sysapi.ErrNotFresh
	}

	// why both lists in the contract: no node-local grant exists any more.
	token, err := mod.apps.ProvisionApp(ctx, slices.Concat(req.GrantPermits, req.ContractPermits))
	if err != nil {
		return nil, err
	}

	if err = mod.appointment.Manager.Set(ctx, token.Identity); err != nil {
		return nil, err
	}
	return token, nil
}

// validateManagerRequest refuses an invalid permit and any policy binding.
func (mod *Module) validateManagerRequest(req *sysapi.RegisterManagerRequest) error {
	for _, p := range slices.Concat(req.GrantPermits, req.ContractPermits) {
		if err := auth.ValidatePermit(p); err != nil {
			return err
		}
	}

	if len(req.Policies) > 0 {
		return errPoliciesRemoved
	}
	return nil
}

// errPoliciesRemoved refuses a request that binds a policy.
var errPoliciesRemoved = errors.New("policies are removed: answer the requests in SwarmState with user.answer_request")
