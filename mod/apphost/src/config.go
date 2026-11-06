package apphost

import (
	"github.com/astralp2p/astrald/lib/paths"
	"github.com/astralp2p/astrald/lib/policyconf"
)

type Config struct {
	// Listen on these addresses
	Listen []string `yaml:"listen,omitempty"`

	// Number of apphost workers
	Workers int `yaml:"workers,omitempty"`

	Tokens map[string]string `yaml:"tokens,omitempty"`

	BindHTTP string `yaml:"bind_http,flow"`

	// Gate selects the ops a session may call by token presence and node state.
	Gate Gate `yaml:"gate,omitempty"`

	// Policies selects the admission policies apphost applies.
	Policies Policies `yaml:"policies,omitempty"`
}

// Gate lists the ops a session may call, IPC, WebSocket and HTTP alike. An
// entry is an op path; the entry "*" matches every op. A session without a
// token reaches only the node, and only the ops Tokenless lists. A query to the
// node, with a token or without, reaches only the ops States lists for the
// node's state. A query with a token to another identity passes the gate.
type Gate struct {
	Tokenless []string   `yaml:"tokenless,omitempty"`
	States    GateStates `yaml:"states,omitempty"`
}

// GateStates holds one op list per node state; see user.Info.State.
type GateStates struct {
	Fresh     []string `yaml:"fresh,omitempty"`
	Unclaimed []string `yaml:"unclaimed,omitempty"`
	Claimed   []string `yaml:"claimed,omitempty"`
}

// Policies holds the config value of each apphost admission policy. A stored
// delegate outranks the value; see GetRegisterAppPolicy.
type Policies struct {
	AppRegister policyconf.Setting `yaml:"app_register,omitempty"`
}

var defaultConfig = Config{
	Listen: []string{
		"tcp:127.0.0.1:8625",
		"unix:~/.apphost.sock",
		"memu:apphosty",
		"memb:apphostb",
	},
	// why: the `/.ws` endpoint accepts any Origin and rests on the bind address
	// to keep other hosts out — .ai/system/topics/ws-transport.md.
	// Exposing the HTTP API beyond the host is an explicit `bind_http`.
	BindHTTP: "tcp:127.0.0.1:8624",
	Tokens:   map[string]string{},
	Workers:  32,
	// why so few: a fresh node only appoints a manager, and an unclaimed node
	// only claims, registers apps and lets them serve; a claimed node leaves the
	// decision to authorization and policies.
	//
	// why apphost.bind, apphost.register_handler and apphost.cancel before the
	// claim: the manager answers the policies delegated to it, an invitation
	// from the owner's other device among them, and an app answers only through
	// a registered handler.
	Gate: Gate{
		Tokenless: []string{
			"apphost.register",
			"apphost.register_manager",
			"user.info",
		},
		States: GateStates{
			Fresh: []string{
				"apphost.register_manager",
				"user.info",
			},
			Unclaimed: []string{
				"user.info",
				"user.derive_identity",
				"user.bootstrap",
				"user.join",
				"apphost.register",
				"apphost.bind",
				"apphost.register_handler",
				"apphost.cancel",
			},
			Claimed: []string{gateAny},
		},
	},
	Policies: Policies{
		AppRegister: policyconf.Setting{Mode: policyconf.RejectAll},
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
