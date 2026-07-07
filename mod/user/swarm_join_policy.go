package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

// SwarmJoinRequestPolicy decides whether to accept an unsolicited join request
// from requester; return true to allow, false to decline.
type SwarmJoinRequestPolicy func(requester *astral.Identity) bool

// SwarmInvitePolicy decides whether to accept an incoming invitation along
// with its accompanying contract; return true to join, false to decline.
type SwarmInvitePolicy func(invitee *astral.Identity, contract *auth.Contract) bool
