package tcp

import (
	"net"
	"testing"
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	tcpmod "github.com/astralp2p/astrald/mod/tcp"
)

// TestLinkKeepaliveConfig pins that tcp.yaml's link_idle_ping and
// link_ping_timeout time the links over tcp conns, each defaulting on its own.
func TestLinkKeepaliveConfig(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		want   exonetmod.LinkKeepalive
	}{
		{"defaults", "", exonetmod.LinkKeepalive{Idle: 15 * time.Second, Timeout: 10 * time.Second}},
		{"idle only", "link_idle_ping: 3s\n", exonetmod.LinkKeepalive{Idle: 3 * time.Second, Timeout: 10 * time.Second}},
		{"both", "link_idle_ping: 1m\nlink_ping_timeout: 20s\n", exonetmod.LinkKeepalive{Idle: time.Minute, Timeout: 20 * time.Second}},
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
			conn := tcpmod.WrapConn(local, true, mod.config.linkKeepalive())
			if got := conn.LinkKeepalive(); got != test.want {
				t.Fatalf("wrapped conn LinkKeepalive() = %v; want %v", got, test.want)
			}
		})
	}
}
