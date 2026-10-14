package apphost

import (
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
)

// policyPath is where apphost persists its policy delegates.
const policyPath = "/mod/apphost/policy"

// policyConfig holds the policy delegates, bound to the tree at policyPath.
//
// why apart from Config: Config is read from apphost.yaml and copied from
// defaultConfig, and a tree.Value carries a mutex, so it cannot ride a copied
// struct.
type policyConfig struct {
	// AppRegister decides apphost.register when set. Unset, the node accepts
	// every registration.
	AppRegister tree.Value[*policy.PolicyDelegate]
}
