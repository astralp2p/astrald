package apphost

import "github.com/cryptopunkscc/astrald/mod/user"

type Config struct {
	// Listen on these addresses
	Listen []string `yaml:"listen,omitempty"`

	// Number of apphost workers
	Workers int `yaml:"workers,omitempty"`

	Tokens map[string]string `yaml:"tokens,omitempty"`

	BindHTTP string `yaml:"bind_http,flow"`

	AllowAnonymous bool `yaml:"allow_anonymous,omitempty"`

	// Web origins recognized as first-party sources of guest queries, each
	// with the permits registration issues to identities from that origin
	TrustedWebSources map[string][]PermitConfig `yaml:"trusted_web_sources,omitempty"`
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
	BindHTTP:       "tcp:0.0.0.0:8624",
	Tokens:         map[string]string{},
	Workers:        32,
	AllowAnonymous: true,
	TrustedWebSources: map[string][]PermitConfig{
		"https://settings.astrald.app": {
			{Action: user.InfoAction{}.ObjectType()},
			{Action: user.ExpelAction{}.ObjectType()},
			{Action: user.AdoptAction{}.ObjectType()},
		},
	},
}
