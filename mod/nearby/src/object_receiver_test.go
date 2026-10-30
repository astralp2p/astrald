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

// newModeModule returns a receiver module whose user is claimed or not and
// whose stored mode is stored, or unset when stored is nil.
func newModeModule(t *testing.T, claimed bool, stored *nearby.Mode) (*Module, *recordingEther) {
	t.Helper()
	nodeID := astral.GenerateIdentity()
	e := &recordingEther{}
	mod := newReceiverModule(nodeID, e)
	if claimed {
		mod.User = claimedUser{id: astral.GenerateIdentity()}
	}
	if stored != nil {
		if err := mod.SetMode(astral.NewContext(nil), *stored); err != nil {
			t.Fatalf("SetMode: %v", err)
		}
	}
	return mod, e
}

// TestModeGatesScanReplyAndScan covers the mode a node acts on and what it
// sends for a scan and for its own scan: a stored Silent holds before and after
// the claim and sends neither; any other mode on an unclaimed node reads as
// Visible; a claimed node keeps its stored mode, and Stealth without
// attachments sends no reply. A reply goes to the scan's source, on-link or not.
func TestModeGatesScanReplyAndScan(t *testing.T) {
	stealth, silent, visible := nearby.ModeStealth, nearby.ModeSilent, nearby.ModeVisible
	cases := []struct {
		name         string
		claimed      bool
		stored       *nearby.Mode
		mode         nearby.Mode
		reply, scans bool
	}{
		{"unclaimed, mode unset", false, nil, visible, true, true},
		{"unclaimed, stored stealth", false, &stealth, visible, true, true},
		{"unclaimed, stored silent", false, &silent, silent, false, false},
		{"claimed, stored silent", true, &silent, silent, false, false},
		{"claimed, stored stealth", true, &stealth, stealth, false, true},
		{"claimed, stored visible", true, &visible, visible, true, true},
	}

	for _, tc := range cases {
		for _, src := range []string{"192.168.1.20", "203.0.113.7"} {
			t.Run(tc.name+" from "+src, func(t *testing.T) {
				mod, e := newModeModule(t, tc.claimed, tc.stored)
				if got := mod.Mode(); got != tc.mode {
					t.Fatalf("Mode() = %v; want %v", got, tc.mode)
				}

				event := &ether.EventBroadcastReceived{SourceIP: ip.IP(net.ParseIP(src)), Object: &nearby.ScanMessage{}}
				if err := mod.ReceiveObject(&recordingDrop{sender: mod.node.Identity(), object: event}); err != nil {
					t.Fatalf("ReceiveObject: %v", err)
				}
				if err := mod.Scan(); err != nil {
					t.Fatalf("Scan: %v", err)
				}

				replies := e.recordedUnicasts()
				if got := len(replies) == 1 && net.IP(replies[0]).Equal(net.ParseIP(src)); got != tc.reply || len(replies) > 1 {
					t.Fatalf("scan replies %v; want a reply to %v: %v", replies, src, tc.reply)
				}
				if got := len(e.recorded()) == 1; got != tc.scans {
					t.Fatalf("own scans %d; want one: %v", len(e.recorded()), tc.scans)
				}
			})
		}
	}
}

// TestReceiveNetworkAddressChangedInSilent covers the address-change path: a
// node in Silent pushes neither its status nor a scan.
func TestReceiveNetworkAddressChangedInSilent(t *testing.T) {
	silent := nearby.ModeSilent
	mod, e := newModeModule(t, true, &silent)

	event := &events.Event{
		ID:       astral.NewNonce(),
		SourceID: mod.node.Identity(),
		Data:     &ip.EventNetworkAddressChanged{Added: []ip.IP{ip.IP(net.ParseIP("192.168.1.30"))}},
	}
	if err := mod.ReceiveObject(&recordingDrop{sender: mod.node.Identity(), object: event}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	if pushed := e.recorded(); len(pushed) != 0 {
		t.Fatalf("pushed %d broadcasts in silent; want none", len(pushed))
	}
}
