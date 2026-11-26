package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// RegisterAppPolicy decides one app registration. apphost.register applies the
// decision: Allow false refuses; otherwise the node writes exactly the permits
// the decision lists and records its authorizer assignments for the new
// identity. A decision whose permits name mod.auth.sudo_action is refused. An error is a question that could not be
// decided, and the node refuses.
//
// The request carries the caller's web origin (empty for local IPC callers),
// the name the app gives itself (unverified), and the permits it asked for on
// each list. The node signs the permits of both lists into one node→app
// contract: portable evidence another node verifies, durable until it
// expires. A grant permit enters it with Delegation zero.
//
// A policy is free to move a permit between the two lists, or to drop it: the
// app states what it wants, the node decides what it holds.
type RegisterAppPolicy func(ctx *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error)

// AcceptAll is the accept_all policy. It admits every registration with the
// permits it asked for, in the list each was asked in.
func AcceptAll(_ *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
	return &apphost.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    req.GrantPermits,
		ContractPermits: req.ContractPermits,
	}, nil
}

// RejectAll is the reject_all policy and the code default: every
// registration is refused.
func RejectAll(_ *astral.Context, _ *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
	return &apphost.AppRegisterDecision{}, nil
}
