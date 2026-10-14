package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// SwarmJoinRequestPolicy decides whether to accept an unsolicited join request.
// A decision with Allow false declines, and so does an error.
type SwarmJoinRequestPolicy func(ctx *astral.Context, req *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error)

// SwarmInvitePolicy decides whether to accept an incoming invitation along with
// its accompanying contract. A decision with Allow false declines, and so does
// an error.
type SwarmInvitePolicy func(ctx *astral.Context, req *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error)
