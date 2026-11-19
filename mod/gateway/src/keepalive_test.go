package gateway

import (
	"testing"
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"gopkg.in/yaml.v2"
)

// TestLinkKeepaliveConfig pins that gateway.yaml's link_idle_ping and
// link_ping_timeout time the links over gw conns, each defaulting on its own.
//
// why UnmarshalStrict: the node loads module configs strictly, so an unknown
// key would stop it.
func TestLinkKeepaliveConfig(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		want   exonetmod.LinkKeepalive
	}{
		{"defaults", "", exonetmod.LinkKeepalive{Idle: 20 * time.Second, Timeout: 15 * time.Second}},
		{"idle only", "link_idle_ping: 40s\n", exonetmod.LinkKeepalive{Idle: 40 * time.Second, Timeout: 15 * time.Second}},
		{"both", "link_idle_ping: 1m\nlink_ping_timeout: 30s\n", exonetmod.LinkKeepalive{Idle: time.Minute, Timeout: 30 * time.Second}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultConfig
			if err := yaml.UnmarshalStrict([]byte(test.config), &cfg); err != nil {
				t.Fatalf("UnmarshalStrict: %v", err)
			}
			if got := cfg.linkKeepalive(); got != test.want {
				t.Fatalf("linkKeepalive() = %v; want %v", got, test.want)
			}
			conn := newGatewayConn(nil, nil, nil, cfg.linkKeepalive())
			if got := conn.LinkKeepalive(); got != test.want {
				t.Fatalf("conn LinkKeepalive() = %v; want %v", got, test.want)
			}
		})
	}
}
