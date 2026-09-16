package nodes

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/events"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// effectTimeout bounds the wait for an effect the receiver starts in a goroutine.
const effectTimeout = 5 * time.Second

// absenceWindow is how long a test watches for an asynchronous effect that must not happen.
const absenceWindow = 200 * time.Millisecond

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// identityConn is an astral.Conn that only answers RemoteIdentity, enough for the link pool to select it.
type identityConn struct {
	astral.Conn
	remote *astral.Identity
}

func (c *identityConn) RemoteIdentity() *astral.Identity { return c.remote }

// recordingEvents records emitted event data.
type recordingEvents struct {
	events.Module

	mu      sync.Mutex
	emitted []astral.Object
}

func (e *recordingEvents) Emit(data astral.Object) *events.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, data)
	return &events.Event{Data: data}
}

func (e *recordingEvents) recorded() []astral.Object {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]astral.Object(nil), e.emitted...)
}

// countingUser counts LocalSwarm calls and reports an empty swarm.
type countingUser struct {
	usermod.Module
	calls atomic.Int32
}

func (u *countingUser) LocalSwarm() []*astral.Identity {
	u.calls.Add(1)
	return nil
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

type receiverFixture struct {
	mod    *Module
	nodeID *astral.Identity
	events *recordingEvents
	user   *countingUser
}

func newReceiverFixture(t *testing.T) *receiverFixture {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f := &receiverFixture{
		nodeID: astral.GenerateIdentity(),
		events: &recordingEvents{},
		user:   &countingUser{},
	}

	f.mod = &Module{
		Deps: Deps{Events: f.events, User: f.user},
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		ctx:  ctx,
	}
	f.mod.linkPool = NewLinkPool(f.mod)

	return f
}

// addLink puts a link with remote into the link pool and returns it.
func (f *receiverFixture) addLink(remote *astral.Identity) *Link {
	link := &Link{id: astral.NewNonce(), conn: &identityConn{remote: remote}}
	f.mod.linkPool.links.Add(link)
	return link
}

func tcpEndpoint(addr string) *tcp.Endpoint {
	return &tcp.Endpoint{IP: ip.IP(net.ParseIP(addr)), Port: 1791}
}

// TestReceiveObservedEndpoint covers endpoint reflection: only a linked peer's
// observation of a public TCP endpoint is recorded; every other message leaves
// no observation, no event and no acceptance.
func TestReceiveObservedEndpoint(t *testing.T) {
	const publicIP = "8.8.8.8"

	cases := []struct {
		name     string
		sender   func(f *receiverFixture) *astral.Identity
		endpoint exonet.Endpoint
		recorded bool
	}{
		{"public TCP endpoint from a linked peer", linkedPeer, tcpEndpoint(publicIP), true},
		{"from an unlinked node", func(*receiverFixture) *astral.Identity { return astral.GenerateIdentity() }, tcpEndpoint(publicIP), false},
		{"from this node", func(f *receiverFixture) *astral.Identity { return f.nodeID }, tcpEndpoint(publicIP), false},
		{"from a zero sender", func(*receiverFixture) *astral.Identity { return nil }, tcpEndpoint(publicIP), false},
		{"private endpoint from a linked peer", linkedPeer, tcpEndpoint("192.168.1.10"), false},
		{"non-TCP endpoint from a linked peer", linkedPeer, &kcp.Endpoint{IP: ip.IP(net.ParseIP(publicIP)), Port: 1791}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReceiverFixture(t)

			drop := &recordingDrop{sender: tc.sender(f), object: &nodes.ObservedEndpointMessage{Endpoint: tc.endpoint}}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			_, observed := f.mod.observedEndpoints.Get(tc.endpoint.Address())
			emitted := f.events.recorded()
			accepted := drop.accepted()

			if !tc.recorded {
				if observed || len(emitted) != 0 || len(accepted) != 0 {
					t.Fatalf("observed=%v events=%d accepts=%v; want no effect", observed, len(emitted), accepted)
				}
				return
			}

			if !observed {
				t.Fatal("the observed endpoint was not recorded")
			}
			if len(emitted) != 1 {
				t.Fatalf("emitted %d events; want one NewObservedEndpointEvent", len(emitted))
			}
			if _, ok := emitted[0].(*nodes.NewObservedEndpointEvent); !ok {
				t.Fatalf("emitted %v; want a NewObservedEndpointEvent", emitted[0].ObjectType())
			}
			if len(accepted) != 1 || accepted[0] {
				t.Fatalf("Accept calls %v; want one Accept(false)", accepted)
			}
		})
	}
}

func linkedPeer(f *receiverFixture) *astral.Identity {
	peer := astral.GenerateIdentity()
	f.addLink(peer)
	return peer
}

// TestReceiveLinkCreatedEventRequiresLocalSender covers the link-created branch:
// only this node's own event consults the swarm for an endpoint refresh.
func TestReceiveLinkCreatedEventRequiresLocalSender(t *testing.T) {
	cases := []struct {
		name       string
		local      bool
		swarmCalls int32
	}{
		{"from another node", false, 0},
		{"from this node", true, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReceiverFixture(t)
			remote := astral.GenerateIdentity()

			sender := remote
			if tc.local {
				sender = f.nodeID
			}

			event := &events.Event{
				ID:       astral.NewNonce(),
				SourceID: f.nodeID,
				Data:     &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: 1},
			}

			drop := &recordingDrop{sender: sender, object: event}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if n := f.user.calls.Load(); n != tc.swarmCalls {
				t.Fatalf("consulted the swarm %d times; want %d", n, tc.swarmCalls)
			}
			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; a link event is never accepted", a)
			}
		})
	}
}

// TestReceiveLinkPressureEventRequiresLocalSender covers the link-pressure branch:
// only this node's own event starts a connectivity upgrade. The peer has a second
// link, so the upgrade reuses it instead of dialing.
func TestReceiveLinkPressureEventRequiresLocalSender(t *testing.T) {
	cases := []struct {
		name    string
		local   bool
		upgrade bool
	}{
		{"from another node", false, false},
		{"from this node", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReceiverFixture(t)
			remote := astral.GenerateIdentity()
			f.addLink(remote)

			sender := remote
			if tc.local {
				sender = f.nodeID
			}

			event := &events.Event{
				ID:       astral.NewNonce(),
				SourceID: f.nodeID,
				Data:     &nodes.LinkPressureEvent{RemoteIdentity: remote, LinkID: astral.NewNonce()},
			}

			drop := &recordingDrop{sender: sender, object: event}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			window := absenceWindow
			if tc.upgrade {
				window = effectTimeout
			}

			if started := waitUpgrade(f.mod, remote, window); started != tc.upgrade {
				t.Fatalf("connectivity upgrade started=%v; want %v", started, tc.upgrade)
			}
			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; a link event is never accepted", a)
			}
		})
	}
}

// waitUpgrade reports whether a connectivity upgrade for remote starts within window.
func waitUpgrade(mod *Module, remote *astral.Identity, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if _, ok := mod.upgraders.Get(remote.String()); ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
