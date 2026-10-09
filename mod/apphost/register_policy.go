package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// AppRegisterPolicy decides what apphost.register issues to the identity it is
// about to provision.
//
// ctx bounds the decision: it ends when the registering query closes. origin
// is the caller's web origin, empty for local IPC callers. name is the name
// the app gives itself, empty when it gives none; it is the app's own claim,
// so a policy shows it beside origin rather than in place of it. caller is the
// identity that called apphost.register, and anonymous is true when its
// session presented no token (caller is then this node's identity).
//
// A permit is the same clause on both rails — an action, its constraints, its
// delegation. What differs is the record it is written into, and the app names
// the record it is asking for. requestedGrantPermits holds what it asked the
// node to record as node-local grants: revocable by deleting a row, never handed
// to anyone, worthless off this node. requestedContractPermits holds what it
// asked to be written into a signed node→app contract: portable evidence another
// node verifies, and durable until it expires.
//
// The trusted-web-source entitlement for origin is joined onto the contract
// request before the policy sees it, because a PermitConfig carries Delegation
// and delegation means nothing to a grant.
//
// The outcome's GrantPermits and ContractPermits are what registration writes
// on each rail, and its Evaluators are the evaluator rules registration sets for
// the new identity. Allow false refuses the registration outright. A policy is free to move a
// permit between the two lists, or to drop it: the app states what it wants, the
// node decides what it holds.
//
// The shipped default grants everything it is handed on the rail it was asked
// for, so a node that cares which apps hold what installs a policy that decides.
type AppRegisterPolicy func(
	ctx *astral.Context,
	origin string,
	name string,
	caller *astral.Identity,
	anonymous bool,
	requestedGrantPermits, requestedContractPermits []*auth.Permit,
) AppRegisterOutcome

// AppRegisterOutcome is what a policy decides for one registration.
type AppRegisterOutcome struct {
	GrantPermits    []*auth.Permit
	ContractPermits []*auth.Permit
	// Evaluators name, per action type, the identity that evaluates the new
	// app's actions. Actor is empty: the identity does not exist until the
	// policy allows, and registration fills it in.
	Evaluators []*authmod.EvaluatorRule
	Allow      bool
}
