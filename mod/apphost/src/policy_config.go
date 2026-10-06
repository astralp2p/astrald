package apphost

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// policyConfig holds the policy delegates, bound to the tree at
// /mod/apphost/policy.
//
// why apart from Config: Config is read from apphost.yaml and copied from
// defaultConfig, and a tree.Value carries a mutex, so it cannot ride a copied
// struct (the log module's setDefaults records the same constraint).
type policyConfig struct {
	// AppRegisterDelegate decides apphost.register when set. Unset, the node
	// accepts every registration.
	AppRegisterDelegate tree.Value[*astral.Identity]
}
