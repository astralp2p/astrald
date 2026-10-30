package apphost

import (
	"github.com/astralp2p/astrald/lib/paths"
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
	// why so few: a session without a token reaches an op as the node. It
	// needs to learn the claim state, claim the node, and register an app;
	// everything else is asked with a token.
	// why .spec: it lists the node's ops and changes nothing, so a session
	// learns what it may call before it holds a token.
	AnonymousAllowlist: AnonymousAllowlist{
		Unclaimed: []string{
			"user.info",            // claim state: no active contract while no user owns the node
			"user.derive_identity", // preview the user that words derive
			"user.bootstrap",       // claim the node from words
			"user.join",            // ask a swarm node for membership
			"apphost.register",     // an app gets its token
			".spec",                // list the node's ops
		},
		Claimed: []string{
			"user.info",
			"apphost.register",
			".spec",
		},
	},
}

// resolveAddrs applies the config path rule to the unix: addresses of listen and bind_http: "~/" is
// expanded, and a relative path fails. A failed address is logged, naming the file and key, and
// dropped; a failed bind_http disables the HTTP server.
//
// why logged and dropped: a listener that fails to bind is logged and skipped as well, and the
// node keeps its other endpoints.
func (mod *Module) resolveAddrs() {
	listen := make([]string, 0, len(mod.config.Listen))
	for i, addr := range mod.config.Listen {
		resolved, err := paths.FromConfigAddr(addr)
		if err != nil {
			mod.log.Error("apphost.yaml listen[%v]: %v", i, err)
			continue
		}
		listen = append(listen, resolved)
	}
	mod.config.Listen = listen

	bindHTTP, err := paths.FromConfigAddr(mod.config.BindHTTP)
	if err != nil {
		mod.log.Error("apphost.yaml bind_http: %v", err)
	}
	mod.config.BindHTTP = bindHTTP
}
