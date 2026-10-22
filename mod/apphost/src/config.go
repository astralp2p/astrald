package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
)

type Config struct {
	// Listen on these addresses
	Listen []string `yaml:"listen,omitempty"`

	// Number of apphost workers
	Workers int `yaml:"workers,omitempty"`

	Tokens map[string]string `yaml:"tokens,omitempty"`

	BindHTTP string `yaml:"bind_http,flow"`

	// AllowAnonymous false refuses every query of a session without a token.
	AllowAnonymous bool `yaml:"allow_anonymous,omitempty"`

	// Web origins recognized as first-party sources of guest queries, each
	// with the permits registration issues to identities from that origin
	TrustedWebSources map[string][]PermitConfig `yaml:"trusted_web_sources,omitempty"`

	// Ops a session without a token may call, by node claim state
	AnonymousAllowlist AnonymousAllowlist `yaml:"anonymous_allowlist,omitempty"`
}

// AnonymousAllowlist lists the ops a session without a token may call, IPC
// and WebSocket alike, selected by whether a user has claimed the node. An op
// absent from the active list is refused. A session with a token is not
// restricted by these lists.
type AnonymousAllowlist struct {
	// Unclaimed applies while no user owns the node (the setup ceremony).
	Unclaimed []string `yaml:"unclaimed,omitempty"`
	// Claimed applies once a user has claimed the node.
	Claimed []string `yaml:"claimed,omitempty"`
}

// PermitConfig is one permit clause in config: an action type plus how many
// delegation hops the permit allows below a contract link carrying it.
type PermitConfig struct {
	Action     string `yaml:"action"`
	Delegation uint8  `yaml:"delegation,omitempty"`
}

var defaultConfig = Config{
	Listen: []string{
		"tcp:127.0.0.1:8625",
		"unix:~/.apphost.sock",
		"memu:apphosty",
		"memb:apphostb",
	},
	// why: the `/.ws` endpoint accepts any Origin and rests on the bind address
	// to keep a remote page out — .ai/system/topics/ws-transport.md.
	// Exposing the HTTP API beyond the host is an explicit `bind_http`.
	BindHTTP:       "tcp:127.0.0.1:8624",
	Tokens:         map[string]string{},
	Workers:        32,
	AllowAnonymous: true,
	// why AdminNetwork: the settings app's LAN scan calls nearby.broadcast and
	// nearby.list, and both ops authorize that action
	// (mod/nearby/src/authorize_admin_network.go).
	// why one test origin: a shipped default entitles its origins on every node
	// that takes the default, so a production origin is the node operator's
	// choice and is named in the settings app's DEPLOY.md, not here.
	TrustedWebSources: map[string][]PermitConfig{
		"https://settings.test.satforge.dev": {
			{Action: user.SeeSwarmAction{}.ObjectType()},
			{Action: user.AdminSwarmAction{}.ObjectType()},
			{Action: auth.AdminNetworkAction{}.ObjectType()},
		},
	},
	// why so few: a session without a token reaches an op as the node. It
	// needs to learn the claim state, claim the node, and register an app;
	// everything else is asked with a token.
	AnonymousAllowlist: AnonymousAllowlist{
		Unclaimed: []string{
			"user.info",            // claim state: code 2 while no user owns the node
			"user.derive_identity", // preview the user that words derive
			"user.bootstrap",       // claim the node from words
			"user.join",            // ask a swarm node for membership
			"crypto.list_keys",     // held public keys
			"apphost.register",     // an app gets its token
			// todo: the step-by-step setup ops leave the list once control-plane
			// and the settings apps claim through user.bootstrap.
			"bip137sig.new_entropy",
			"bip137sig.mnemonic",
			"bip137sig.seed",
			"bip137sig.derive_key",
			"coldcard.scan",
			"crypto.public_key",
			"objects.store",
			"user.new_node_contract",
			"auth.sign_contract",
			"user.accept_contract",
		},
		Claimed: []string{
			"user.info",
			"apphost.register",
		},
	},
}
