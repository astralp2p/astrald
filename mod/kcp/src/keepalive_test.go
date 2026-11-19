package kcp

import (
	"testing"
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// TestLinkKeepaliveConfig pins that kcp.yaml's link_idle_ping and
// link_ping_timeout time the links over kcp conns, each defaulting on its own.
func TestLinkKeepaliveConfig(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		want   exonetmod.LinkKeepalive
	}{
		{"defaults", "", exonetmod.LinkKeepalive{Idle: 15 * time.Second, Timeout: 10 * time.Second}},
		{"timeout only", "link_ping_timeout: 4s\n", exonetmod.LinkKeepalive{Idle: 15 * time.Second, Timeout: 4 * time.Second}},
		{"both", "link_idle_ping: 45s\nlink_ping_timeout: 20s\n", exonetmod.LinkKeepalive{Idle: 45 * time.Second, Timeout: 20 * time.Second}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := loadModule(t, test.config)
			if got := mod.config.linkKeepalive(); got != test.want {
				t.Fatalf("linkKeepalive() = %v; want %v", got, test.want)
			}
			conn := WrapKCPConn(nil, nil, nil, true, time.Minute)
			conn.keepalive = mod.config.linkKeepalive()
			var kc exonetmod.KeepaliveConn = conn
			if got := kc.LinkKeepalive(); got != test.want {
				t.Fatalf("conn LinkKeepalive() = %v; want %v", got, test.want)
			}
		})
	}
}
