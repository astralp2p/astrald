package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// SwarmJoinPolicy decides whether user.request_membership admits an
// unsolicited join request into this node's swarm. Allow false declines. An
// error is a question that could not be decided, and the node declines.
type SwarmJoinPolicy func(ctx *astral.Context, req *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error)

// SwarmInvitePolicy decides whether user.accept_membership accepts an
// incoming invitation and the membership contract it offers. Allow false
// declines. An error is a question that could not be decided, and the node
// declines.
type SwarmInvitePolicy func(ctx *astral.Context, req *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error)

// AcceptAllJoins is the bundled join policy: every requester is admitted.
func AcceptAllJoins(_ *astral.Context, _ *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error) {
	return &user.SwarmJoinDecision{Allow: true}, nil
}

// AcceptAllInvites is the bundled invite policy: every invitation is accepted.
func AcceptAllInvites(_ *astral.Context, _ *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error) {
	return &user.SwarmInviteDecision{Allow: true}, nil
}
