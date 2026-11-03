package user

import (
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
}

// Policies holds the config value of each user admission policy. A stored
// delegate outranks the value; see GetSwarmJoinPolicy.
type Policies struct {
	SwarmJoin   policyconf.Setting `yaml:"swarm_join,omitempty"`
	SwarmInvite policyconf.Setting `yaml:"swarm_invite,omitempty"`
}

var defaultFileConfig = FileConfig{
	Policies: Policies{
		SwarmJoin:   policyconf.Setting{Mode: policyconf.RejectAll},
		SwarmInvite: policyconf.Setting{Mode: policyconf.RejectAll},
	},
}
