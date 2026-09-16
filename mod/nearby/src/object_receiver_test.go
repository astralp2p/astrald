package nearby

import (
	"net"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/ether"
	"github.com/astralp2p/astrald/mod/nearby"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// unclaimedUser is a user module with no user, so the node broadcasts in visible mode.
type unclaimedUser struct {
	usermod.Module
}

func (unclaimedUser) Identity() *astral.Identity { return nil }

// recordingEther records broadcast pushes.
type recordingEther struct {
	ether.Module

	mu     sync.Mutex
	pushed []astral.Object
}

func (e *recordingEther) Push(object astral.Object, _ *astral.Identity) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pushed = append(e.pushed, object)
	return nil
}

func (e *recordingEther) recorded() []astral.Object {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]astral.Object(nil), e.pushed...)
}

// recordingDrop is an objects.Drop that records its Accept calls.
type recordingDrop struct {
	sender *astral.Identity
	object astral.Object

	mu      sync.Mutex
	accepts []bool
}

func (d *recordingDrop) SenderID() *astral.Identity { return d.sender }
func (d *recordingDrop) Object() astral.Object      { return d.object }

func (d *recordingDrop) Accept(save bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accepts = append(d.accepts, save)
	return nil
}

func (d *recordingDrop) accepted() []bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]bool(nil), d.accepts...)
}

func newReceiverModule(nodeID *astral.Identity, e *recordingEther) *Module {
	return &Module{
		Deps: Deps{Ether: e, User: unclaimedUser{}},
		node: &identityNode{id: nodeID},
		log:  log.New(nodeID),
	}
}

// TestReceiveBroadcastEventRequiresLocalSender covers the broadcast wrapper: ether
// emits it locally, so only this node's wrapper caches the sender's status. A
// status is never accepted, because the cache is its only effect.
func TestReceiveBroadcastEventRequiresLocalSender(t *testing.T) {
	sourceIP := ip.IP(net.ParseIP("192.168.1.20"))

	cases := []struct {
		name   string
		local  bool
		cached bool
	}{
		{"from another node", false, false},
		{"from this node", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := astral.GenerateIdentity()
			mod := newReceiverModule(nodeID, &recordingEther{})

			sender := astral.GenerateIdentity()
			if tc.local {
				sender = nodeID
			}

			event := &ether.EventBroadcastReceived{
				SourceIP: sourceIP,
				Object:   &nearby.StatusMessage{Attachments: astral.NewBundle()},
			}

			drop := &recordingDrop{sender: sender, object: event}
			if err := mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if _, cached := mod.cache.Get(sourceIP.String()); cached != tc.cached {
				t.Fatalf("status cached=%v; want %v", cached, tc.cached)
			}

			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; a status is never accepted", a)
			}
		})
	}
}

// TestReceiveNetworkAddressChangedRequiresLocalSender covers the network-address
// event: only this node's own event broadcasts its status and scans.
func TestReceiveNetworkAddressChangedRequiresLocalSender(t *testing.T) {
	cases := []struct {
		name   string
		local  bool
		pushes int
	}{
		{"from another node", false, 0},
		{"from this node", true, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := astral.GenerateIdentity()
			e := &recordingEther{}
			mod := newReceiverModule(nodeID, e)

			sender := astral.GenerateIdentity()
			if tc.local {
				sender = nodeID
			}

			event := &ip.EventNetworkAddressChanged{Added: []ip.IP{ip.IP(net.ParseIP("192.168.1.30"))}}

			drop := &recordingDrop{sender: sender, object: event}
			if err := mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			pushed := e.recorded()
			if len(pushed) != tc.pushes {
				t.Fatalf("pushed %d broadcasts; want %d", len(pushed), tc.pushes)
			}

			accepted := drop.accepted()
			if !tc.local {
				if len(accepted) != 0 {
					t.Fatalf("Accept calls %v; want none", accepted)
				}
				return
			}

			if _, ok := pushed[0].(*nearby.StatusMessage); !ok {
				t.Fatalf("first push is %v; want the status", pushed[0].ObjectType())
			}
			if _, ok := pushed[1].(*nearby.ScanMessage); !ok {
				t.Fatalf("second push is %v; want a scan", pushed[1].ObjectType())
			}
			if len(accepted) != 1 || accepted[0] {
				t.Fatalf("Accept calls %v; want one Accept(false)", accepted)
			}
		})
	}
}
