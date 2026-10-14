package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// AppRegisterPolicy decides what apphost.register issues to the identity it is
// about to provision.
//
// A permit is the same clause on both rails — an action, its constraints, its
// delegation. What differs is the record it is written into, and the app names
// the record it is asking for: the request's GrantPermits ask for node-local
// grants, its ContractPermits for a signed node→app contract. The
// trusted-web-source entitlement for the origin is joined onto the contract
// request before the policy sees it, because a PermitConfig carries Delegation
// and delegation means nothing to a grant.
//
// The decision's GrantPermits and ContractPermits are what registration writes
// on each rail, and its Authorizers are assigned to the new app. Allow false
// refuses the registration. A policy is free to move a permit between the two
// lists, or to drop it: the app states what it wants, the node decides what it
// holds. An error refuses as well.
//
// The shipped default grants everything it is handed on the rail it was asked
// for, so a node that cares which apps hold what delegates the decision.
type AppRegisterPolicy func(ctx *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error)
