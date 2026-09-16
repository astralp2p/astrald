package nat

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// publicIPs reports one public IP candidate and counts the calls.
type publicIPs struct {
	ipmod.Module
	calls atomic.Int32
}

func (p *publicIPs) PublicIPCandidates() []ip.IP {
	p.calls.Add(1)
	return []ip.IP{ip.IP(net.ParseIP("8.8.8.8"))}
}

// acceptCounter is an objects.Drop that counts its Accept calls.
type acceptCounter struct {
	sender  *astral.Identity
	object  astral.Object
	accepts atomic.Int32
}

func (d *acceptCounter) SenderID() *astral.Identity { return d.sender }
func (d *acceptCounter) Object() astral.Object      { return d.object }
func (d *acceptCounter) Accept(bool) error {
	d.accepts.Add(1)
	return nil
}

// TestReceiveNewObservedEndpointEventRequiresLocalSender covers the only branch of
// the nat receiver: this node's own event re-evaluates the enabled state, and the
// same event from another node changes nothing.
func TestReceiveNewObservedEndpointEventRequiresLocalSender(t *testing.T) {
	cases := []struct {
		name    string
		local   bool
		enabled bool
	}{
		{"from another node", false, false},
		{"from this node", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := astral.GenerateIdentity()
			ips := &publicIPs{}
			mod := &Module{
				Deps:     Deps{IP: ips},
				node:     &identityNode{id: nodeID},
				settings: Settings{Enabled: &tree.Value[*astral.Bool]{}},
				cond:     sync.NewCond(&sync.Mutex{}),
			}

			sender := astral.GenerateIdentity()
			if tc.local {
				sender = nodeID
			}

			event := &events.Event{ID: astral.NewNonce(), SourceID: nodeID, Data: &nodes.NewObservedEndpointEvent{}}
			drop := &acceptCounter{sender: sender, object: event}

			if err := mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if got := mod.enabled.Load(); got != tc.enabled {
				t.Fatalf("enabled=%v; want %v", got, tc.enabled)
			}

			wantCalls := int32(0)
			if tc.local {
				wantCalls = 1
			}
			if n := ips.calls.Load(); n != wantCalls {
				t.Fatalf("read public IP candidates %d times; want %d", n, wantCalls)
			}

			if n := drop.accepts.Load(); n != 0 {
				t.Fatalf("accepted the event %d times; want none", n)
			}
		})
	}
}
