package user

import (
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
)

// policyPath is where the user module persists its policy delegates.
const policyPath = "/mod/user/policy"

// policyConfig holds the swarm policy delegates, bound to the tree at
// policyPath. Unset, the node accepts every join request and invitation.
type policyConfig struct {
	SwarmJoin   tree.Value[*policy.PolicyDelegate]
	SwarmInvite tree.Value[*policy.PolicyDelegate]
}
