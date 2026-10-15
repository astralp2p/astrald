package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// RegisterAppPolicy decides one app registration. apphost.register applies the
// decision: Allow false refuses; otherwise the node writes exactly the permits
// the decision lists, on the rail each list names, and records its authorizer
// assignments for the new identity. An error is a question that could not be
// decided, and the node refuses.
//
// The request carries the caller's web origin (empty for local IPC callers),
// the name the app gives itself (unverified), and the permits it asked for on
// each rail. A permit is the same clause on both rails; what differs is the
// record it is written into. GrantPermits are node-local grants: revocable by
// deleting a row, never handed to anyone, worthless off this node.
// ContractPermits go into a signed node→app contract: portable evidence
// another node verifies, durable until it expires. The trusted-web-source
// entitlement for the origin is joined onto the contract request before the
// policy sees it, because a PermitConfig carries Delegation and delegation
// means nothing to a grant.
//
// A policy is free to move a permit between the two lists, or to drop it: the
// app states what it wants, the node decides what it holds.
type RegisterAppPolicy func(ctx *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error)

// AcceptAll is the bundled policy. It admits every registration with the
// permits it asked for, on the rail each list names, whether the caller's
// origin entitled it or the app simply asked. A node that cares which apps
// hold what delegates the decision.
func AcceptAll(_ *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
	return &apphost.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    req.GrantPermits,
		ContractPermits: req.ContractPermits,
	}, nil
}
