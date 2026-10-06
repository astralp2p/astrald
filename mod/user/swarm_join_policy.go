package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// SwarmJoinRequestPolicy decides whether to accept an unsolicited join request
// from requester; return true to allow, false to decline. ctx ends when the
// requesting query closes.
type SwarmJoinRequestPolicy func(ctx *astral.Context, requester *astral.Identity) bool

// SwarmInvitePolicy decides whether to accept an incoming invitation from
// inviter along with its accompanying contract; return true to join, false to
// decline. ctx ends when the inviting query closes.
type SwarmInvitePolicy func(ctx *astral.Context, inviter *astral.Identity, contract *auth.Contract) bool
