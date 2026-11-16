package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// GrantRequestPolicy decides one apphost.request_grant. Allow false, or an
// error, refuses; otherwise the node grants the requester every permit the
// request lists.
type GrantRequestPolicy func(ctx *astral.Context, req *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error)

// RefuseAllGrants is the reject_all grant request policy and the code default:
// an app never grants itself authority.
func RefuseAllGrants(_ *astral.Context, _ *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error) {
	return &apphost.GrantDecision{}, nil
}

// AcceptAllGrants is the accept_all grant request policy: every request that
// passes permit validation is granted. It lets any app grant itself any action
// it names, so the node applies it only while policies.grant_request is
// accept_all.
func AcceptAllGrants(_ *astral.Context, _ *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error) {
	return &apphost.GrantDecision{Allow: true}, nil
}
