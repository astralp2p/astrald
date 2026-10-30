package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
)

// GrantRequestPolicy decides one apphost.request_grant. Allow false, or an
// error, refuses; otherwise the node grants the requester every permit the
// request lists.
type GrantRequestPolicy func(ctx *astral.Context, req *apphost.GrantRequest) (*apphost.GrantDecision, error)

// RefuseAllGrants is the bundled policy: an app never grants itself authority.
// A node that lets apps ask delegates the decision.
func RefuseAllGrants(_ *astral.Context, _ *apphost.GrantRequest) (*apphost.GrantDecision, error) {
	return &apphost.GrantDecision{}, nil
}
