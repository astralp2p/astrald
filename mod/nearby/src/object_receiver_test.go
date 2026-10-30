package nearby

import (
	"net"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	ethermod "github.com/astralp2p/astrald/mod/ether"
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

// recordingEther records broadcast pushes and the addresses of unicast pushes.
type recordingEther struct {
	ethermod.Module

	mu       sync.Mutex
	pushed   []astral.Object
	unicasts []ip.IP
}

func (e *recordingEther) Push(object astral.Object) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pushed = append(e.pushed, object)
	return nil
}

func (e *recordingEther) PushToIP(addr ip.IP, _ astral.Object) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unicasts = append(e.unicasts, addr)
	return nil
}

func (e *recordingEther) recorded() []astral.Object {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]astral.Object(nil), e.pushed...)
}

func (e *recordingEther) recordedUnicasts() []ip.IP {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]ip.IP(nil), e.unicasts...)
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

			event := &events.Event{
				ID:       astral.NewNonce(),
				SourceID: nodeID,
				Data:     &ip.EventNetworkAddressChanged{Added: []ip.IP{ip.IP(net.ParseIP("192.168.1.30"))}},
			}

			drop := &recordingDrop{sender: sender, object: event}
			if err := mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			pushed := e.recorded()
			if len(pushed) != tc.pushes {
				t.Fatalf("pushed %d broadcasts; want %d", len(pushed), tc.pushes)
			}

			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; an event is never accepted", a)
			}
			if !tc.local {
				return
			}

			if _, ok := pushed[0].(*nearby.StatusMessage); !ok {
				t.Fatalf("first push is %v; want the status", pushed[0].ObjectType())
			}
			if _, ok := pushed[1].(*nearby.ScanMessage); !ok {
				t.Fatalf("second push is %v; want a scan", pushed[1].ObjectType())
			}
		})
	}
}

// TestSilentSendsNothing covers the mode a node acts on and what it sends: a
// stored Silent holds before and after the claim and sends no scan reply and
// no scan of its own; any other mode on an unclaimed node reads as Visible and
// answers a scan from any source, on-link or not.
func TestSilentSendsNothing(t *testing.T) {
	stealth, silent := nearby.ModeStealth, nearby.ModeSilent
	sources := []ip.IP{
		ip.IP(net.ParseIP("192.168.1.20")),
		ip.IP(net.ParseIP("203.0.113.7")),
	}

	cases := []struct {
		name    string
		claimed bool
		stored  *nearby.Mode
		mode    nearby.Mode
		sends   bool
	}{
		{"unclaimed, mode unset", false, nil, nearby.ModeVisible, true},
		{"unclaimed, stored stealth", false, &stealth, nearby.ModeVisible, true},
		{"unclaimed, stored silent", false, &silent, nearby.ModeSilent, false},
		{"claimed, stored silent", true, &silent, nearby.ModeSilent, false},
	}

	for _, tc := range cases {
		for _, src := range sources {
			t.Run(tc.name+" from "+src.String(), func(t *testing.T) {
				nodeID := astral.GenerateIdentity()
				e := &recordingEther{}
				mod := newReceiverModule(nodeID, e)
				if tc.claimed {
					mod.User = claimedUser{id: astral.GenerateIdentity()}
				}
				if tc.stored != nil {
					if err := mod.SetMode(astral.NewContext(nil), *tc.stored); err != nil {
						t.Fatalf("SetMode: %v", err)
					}
				}

				if got := mod.Mode(); got != tc.mode {
					t.Fatalf("Mode() = %v; want %v", got, tc.mode)
				}

				event := &ether.EventBroadcastReceived{SourceIP: src, Object: &nearby.ScanMessage{}}
				drop := &recordingDrop{sender: nodeID, object: event}
				if err := mod.ReceiveObject(drop); err != nil {
					t.Fatalf("ReceiveObject: %v", err)
				}
				if err := mod.Scan(); err != nil {
					t.Fatalf("Scan: %v", err)
				}

				replies := e.recordedUnicasts()
				switch {
				case tc.sends && (len(replies) != 1 || !net.IP(replies[0]).Equal(net.IP(src))):
					t.Fatalf("scan replies %v; want one to %v", replies, src)
				case !tc.sends && len(replies) != 0:
					t.Fatalf("scan replies %v; want none", replies)
				}

				scans := len(e.recorded())
				switch {
				case tc.sends && scans != 1:
					t.Fatalf("own scans %d; want 1", scans)
				case !tc.sends && scans != 0:
					t.Fatalf("own scans %d; want none", scans)
				}
			})
		}
	}
}
