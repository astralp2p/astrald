package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// GrantRequestPolicy decides one apphost.request_grant. Allow false, or an
// error, refuses; otherwise the node grants the requester every permit the
// request lists.
type GrantRequestPolicy func(ctx *astral.Context, req *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error)

// The grant request policy modes apphost.set_grant_request_policy takes as
// mode. A stored delegate selects the delegate mode; with neither stored the
// node applies GrantRequestRejectAll.
const (
	GrantRequestAcceptAll = "accept_all"
	GrantRequestRejectAll = "reject_all"
)

// RefuseAllGrants is the bundled policy and the reject_all mode: an app never
// grants itself authority.
func RefuseAllGrants(_ *astral.Context, _ *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error) {
	return &apphost.GrantDecision{}, nil
}

// AcceptAllGrants is the accept_all mode: every request that passes permit
// validation is granted. It lets any app grant itself any action it names.
func AcceptAllGrants(_ *astral.Context, _ *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error) {
	return &apphost.GrantDecision{Allow: true}, nil
}
