package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Config is bound to the tree at /mod/user/config.
//
// Owned is set once a user contract activates and is never cleared by expiry
// or expulsion; see WasOwned.
//
// User is the user identity, the root key, when it is not the active
// contract's issuer: a node a carrier admitted holds a membership the carrier
// signed. PendingJoin is the JoinRequest this node keeps until its join ends
// (user.join), so a restart resumes the same request.
type Config struct {
	ActiveContract tree.Value[*auth.SignedContract]
	Owned          tree.Value[*astral.Bool]
	User           tree.Value[*astral.Identity]
	PendingJoin    tree.Value[*user.SignedJoinRequest]
}

// FileConfig is read from user.yaml.
type FileConfig struct {
	// ReconcileInterval is the period of the expulsion sweep; see runReconcile.
	ReconcileInterval time.Duration `yaml:"reconcile_interval,omitempty"`
	// SiblingSyncInterval is the period of the sibling sync; zero selects the
	// default and a negative value turns it off. See runPeriodicSiblingSync.
	SiblingSyncInterval time.Duration `yaml:"sibling_sync_interval,omitempty"`
	// SiblingSyncDisabled turns the sibling sync off.
	SiblingSyncDisabled bool `yaml:"sibling_sync_disabled,omitempty"`
}

var defaultFileConfig = FileConfig{
	ReconcileInterval:   defaultReconcileInterval,
	SiblingSyncInterval: defaultSiblingSyncInterval,
}
