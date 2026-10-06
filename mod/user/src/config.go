package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

type Config struct {
	ActiveContract tree.Value[*auth.SignedContract]

	// SwarmJoinDelegate decides user.request_membership when set. Unset, the
	// node accepts every join request.
	SwarmJoinDelegate tree.Value[*astral.Identity]

	// SwarmInviteDelegate decides user.accept_membership when set. Unset, the
	// node accepts every invitation.
	SwarmInviteDelegate tree.Value[*astral.Identity]
}
