package apphost

import "github.com/astralp2p/astral-go/api/auth"

// AppRegisterPolicy decides what apphost.register issues to the identity it is
// about to provision.
//
// origin is the caller's web origin, empty for local IPC callers.
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
// grantPermits and contractPermits are what registration writes on each rail.
// Returning false refuses the registration outright. A policy is free to move a
// permit between the two lists, or to drop it: the app states what it wants, the
// node decides what it holds.
//
// The shipped default grants everything it is handed on the rail it was asked
// for, so a node that cares which apps hold what installs a policy that decides.
type AppRegisterPolicy func(
	origin string,
	requestedGrantPermits, requestedContractPermits []*auth.Permit,
) (grantPermits, contractPermits []*auth.Permit, allow bool)
