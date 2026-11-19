package nodes

import (
	"net"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
)

// A hole where the active side sits behind a NAT that maps its socket 38826 to
// 6635: kcp binds each side's bind port and dials the other side's external endpoint.
func TestNewHolePorts_BindsBindPortsAndDialsExternalEndpoints(t *testing.T) {
	natted, public := astral.GenerateIdentity(), astral.GenerateIdentity()
	natIP := ip.IP(net.ParseIP("212.105.131.237").To4())
	publicIP := ip.IP(net.ParseIP("45.59.119.13").To4())

	hole := &nat.Hole{
		ActiveIdentity:  natted,
		ActiveEndpoint:  nat.Endpoint{IP: natIP, Port: 6635},
		ActiveBindPort:  38826,
		PassiveIdentity: public,
		PassiveEndpoint: nat.Endpoint{IP: publicIP, Port: 55001},
		PassiveBindPort: 55001,
	}

	tests := []struct {
		name string
		self *astral.Identity
		want holePorts
	}{
		{"natted side", natted, holePorts{
			localEndpoint: kcp.Endpoint{IP: natIP, Port: 6635}, localBindPort: 38826,
			peerEndpoint: kcp.Endpoint{IP: publicIP, Port: 55001}, peerBindPort: 55001,
		}},
		{"public side", public, holePorts{
			localEndpoint: kcp.Endpoint{IP: publicIP, Port: 55001}, localBindPort: 55001,
			peerEndpoint: kcp.Endpoint{IP: natIP, Port: 6635}, peerBindPort: 38826,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newHolePorts(hole, tt.self)
			if got.localBindPort != tt.want.localBindPort || got.peerBindPort != tt.want.peerBindPort {
				t.Errorf("bind ports local %d peer %d; want local %d peer %d",
					got.localBindPort, got.peerBindPort, tt.want.localBindPort, tt.want.peerBindPort)
			}
			if got.localEndpoint.Address() != tt.want.localEndpoint.Address() ||
				got.peerEndpoint.Address() != tt.want.peerEndpoint.Address() {
				t.Errorf("endpoints local %v peer %v; want local %v peer %v",
					got.localEndpoint.Address(), got.peerEndpoint.Address(),
					tt.want.localEndpoint.Address(), tt.want.peerEndpoint.Address())
			}
		})
	}
}
