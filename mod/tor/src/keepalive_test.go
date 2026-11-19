package tor

import (
	"net"
	"testing"
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// TestLinkKeepaliveConfig pins that tor.yaml's link_idle_ping and
// link_ping_timeout time the links over tor conns, each defaulting on its own.
func TestLinkKeepaliveConfig(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		want   exonetmod.LinkKeepalive
	}{
		{"defaults", "", exonetmod.LinkKeepalive{Idle: 30 * time.Second, Timeout: 30 * time.Second}},
		{"idle only", "link_idle_ping: 1m\n", exonetmod.LinkKeepalive{Idle: time.Minute, Timeout: 30 * time.Second}},
		{"both", "link_idle_ping: 2m\nlink_ping_timeout: 45s\n", exonetmod.LinkKeepalive{Idle: 2 * time.Minute, Timeout: 45 * time.Second}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := loadModule(t, test.config)
			if got := mod.config.linkKeepalive(); got != test.want {
				t.Fatalf("linkKeepalive() = %v; want %v", got, test.want)
			}

			local, remote := net.Pipe()
			defer local.Close()
			defer remote.Close()
			conn := newConn(local, nil, true)
			conn.keepalive = mod.config.linkKeepalive()
			if got := conn.LinkKeepalive(); got != test.want {
				t.Fatalf("conn LinkKeepalive() = %v; want %v", got, test.want)
			}
		})
	}
}
