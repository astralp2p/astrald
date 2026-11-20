package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/lib/policyconf"
)

// Config is bound to the tree at /mod/user/config.
//
// Owned is set once a user contract activates and is never cleared by expiry
// or expulsion; see WasOwned.
type Config struct {
	ActiveContract tree.Value[*auth.SignedContract]
	Owned          tree.Value[*astral.Bool]
}

// FileConfig is read from user.yaml.
type FileConfig struct {
	// Policies selects the admission policies the user module applies.
	Policies Policies `yaml:"policies,omitempty"`
	// ReconcileInterval is the period of the expulsion sweep; see runReconcile.
	ReconcileInterval time.Duration `yaml:"reconcile_interval,omitempty"`
	// SiblingSyncInterval is the period of the sibling sync; zero selects the
	// default and a negative value turns it off. See runPeriodicSiblingSync.
	SiblingSyncInterval time.Duration `yaml:"sibling_sync_interval,omitempty"`
	// SiblingSyncDisabled turns the sibling sync off.
	SiblingSyncDisabled bool `yaml:"sibling_sync_disabled,omitempty"`
}

// Policies holds the config value of each user admission policy. A stored
// delegate outranks the value; see GetSwarmJoinPolicy.
type Policies struct {
	SwarmJoin      policyconf.Setting `yaml:"swarm_join,omitempty"`
	SwarmInvite    policyconf.Setting `yaml:"swarm_invite,omitempty"`
	ContractAccept policyconf.Setting `yaml:"contract_accept,omitempty"`
}

var defaultFileConfig = FileConfig{
	Policies: Policies{
		SwarmJoin:      policyconf.Setting{Mode: policyconf.RejectAll},
		SwarmInvite:    policyconf.Setting{Mode: policyconf.RejectAll},
		ContractAccept: policyconf.Setting{Mode: policyconf.RejectAll},
	},
	ReconcileInterval:   defaultReconcileInterval,
	SiblingSyncInterval: defaultSiblingSyncInterval,
}
