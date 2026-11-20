package nodes

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// swarmAnnounceDelay replaces the 5 s announcement wait in the swarm tests.
const swarmAnnounceDelay = 500 * time.Millisecond

// errNotLinked is the outcome of a push or pull between two nodes with no link.
var errNotLinked = errors.New("not linked")

// testSwarm is an in-process local swarm of nodes modules: a push or an
// endpoint pull between two nodes reaches the other module directly, and only
// over a link both nodes pool.
type testSwarm struct {
	nodes []*swarmNode
}

// swarmNode is one node of a testSwarm.
type swarmNode struct {
	swarm  *testSwarm
	id     *astral.Identity
	mod    *Module
	events *recordingEvents
	// announced records the PushToSiblings calls of this node.
	announced pushLog
	// pulled records the remote resolve calls this node made.
	pulled []pull
	mu     sync.Mutex
	// own is the address set this node serves for itself, as mod/ip and mod/tcp would.
	own []string
}

func newTestSwarm(t *testing.T, size int) *testSwarm {
	t.Helper()
	s := &testSwarm{}
	for i := 0; i < size; i++ {
		s.nodes = append(s.nodes, s.newNode(t))
	}
	return s
}

func (s *testSwarm) newNode(t *testing.T) *swarmNode {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	n := &swarmNode{swarm: s, id: astral.GenerateIdentity(), events: &recordingEvents{}}
	n.mod = &Module{
		Deps: Deps{
			Events:  n.events,
			User:    &swarmNodeUser{node: n},
			Objects: &swarmNodeObjects{node: n},
		},
		node:          &identityNode{id: n.id},
		log:           log.New(n.id),
		ctx:           ctx,
		db:            testEndpointDB(t),
		announceDelay: swarmAnnounceDelay,
	}
	n.mod.resolveRemote = n.resolveRemote
	n.mod.linkPool = NewLinkPool(n.mod)
	return n
}

// members returns the identity of every node in the swarm.
func (s *testSwarm) members() (ids []*astral.Identity) {
	for _, n := range s.nodes {
		ids = append(ids, n.id)
	}
	return
}

func (s *testSwarm) find(id *astral.Identity) *swarmNode {
	for _, n := range s.nodes {
		if n.id.IsEqual(id) {
			return n
		}
	}
	return nil
}

// link pools a link between a and b on both nodes.
func (s *testSwarm) link(a, b *swarmNode) {
	a.mod.linkPool.links.Add(&Link{id: astral.NewNonce(), conn: &identityConn{remote: b.id}})
	b.mod.linkPool.links.Add(&Link{id: astral.NewNonce(), conn: &identityConn{remote: a.id}})
}

// deliver hands obj from sender to the node target over their link.
func (s *testSwarm) deliver(sender *swarmNode, target *astral.Identity, obj astral.Object) error {
	to := s.find(target)
	if to == nil || !sender.mod.IsLinked(target) {
		return errNotLinked
	}
	return to.mod.ReceiveObject(&recordingDrop{sender: sender.id, object: obj})
}

// swarmNodeUser is the user module of a swarm node: the local swarm is every
// node, and PushToSiblings reaches every linked one.
type swarmNodeUser struct {
	usermod.Module
	node *swarmNode
}

func (u *swarmNodeUser) LocalSwarm() []*astral.Identity { return u.node.swarm.members() }

func (u *swarmNodeUser) PushToSiblings(_ *astral.Context, obj astral.Object) {
	u.node.announced.add(push{object: obj})
	for _, id := range u.node.swarm.members() {
		if !id.IsEqual(u.node.id) {
			_ = u.node.swarm.deliver(u.node, id, obj)
		}
	}
}

// swarmNodeObjects is the objects module of a swarm node: a push reaches a linked node.
type swarmNodeObjects struct {
	objectsmod.Module
	node *swarmNode
}

func (o *swarmNodeObjects) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	return o.node.swarm.deliver(o.node, target, obj)
}

// resolveRemote answers nodes.resolve_endpoints from the resolver node over a
// link: its own address set for itself, its stored rows for another node.
func (n *swarmNode) resolveRemote(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	n.mu.Lock()
	n.pulled = append(n.pulled, pull{resolver: resolver, identity: identity})
	n.mu.Unlock()

	r := n.swarm.find(resolver)
	if r == nil || !n.mod.IsLinked(resolver) {
		return nil, errNotLinked
	}
	if identity.IsEqual(resolver) {
		return r.served(), nil
	}
	return r.stored(identity)
}

func (n *swarmNode) setOwn(addrs ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.own = addrs
}

func (n *swarmNode) served() []*nodes.EndpointWithTTL {
	n.mu.Lock()
	defer n.mu.Unlock()
	return endpointsWithTTL(n.own...)
}

// stored returns the unexpired rows this node holds for identity.
func (n *swarmNode) stored(identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	rows, err := n.mod.db.FindEndpoints(identity)
	if err != nil {
		return nil, err
	}
	var list []*nodes.EndpointWithTTL
	for _, row := range rows {
		ep, err := tcp.ParseEndpoint(row.Address)
		if err != nil {
			return nil, err
		}
		list = append(list, nodes.NewEndpointWithTTL(ep, time.Until(*row.ExpiresAt)))
	}
	return list, nil
}

// holds reports whether this node stores an unexpired row for addr of other.
func (n *swarmNode) holds(other *swarmNode, addr string) bool {
	rows, err := n.mod.db.FindEndpoints(other.id)
	if err != nil {
		return false
	}
	want := tcpEndpoint(addr).Address()
	for _, row := range rows {
		if row.Address == want {
			return true
		}
	}
	return false
}

func (n *swarmNode) pulls() []pull {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]pull(nil), n.pulled...)
}

// changeAddress reports a local address change to addr, as mod/ip emits it.
func (n *swarmNode) changeAddress(t *testing.T, addr string) {
	t.Helper()
	n.setOwn(addr)
	if err := n.mod.ReceiveObject(&recordingDrop{sender: n.id, object: addressChangedTo(n.id, addr)}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
}

// updatedFor counts the EndpointsUpdatedEvents this node emitted for other.
func (n *swarmNode) updatedFor(other *swarmNode) (count int) {
	for _, e := range n.events.recorded() {
		if u, ok := e.(*nodes.EndpointsUpdatedEvent); ok && u.Identity.IsEqual(other.id) {
			count++
		}
	}
	return
}

// natSwarm builds the netsim A/B/C lab: A behind NAT, C public, B the mover.
// A and B share no link: B cannot dial A behind its NAT, and A dials B's old
// address alone. A and C hold B's old address.
func natSwarm(t *testing.T) (a, b, c *swarmNode) {
	t.Helper()
	s := newTestSwarm(t, 3)
	a, b, c = s.nodes[0], s.nodes[1], s.nodes[2]
	s.link(a, c)
	s.link(b, c)

	b.setOwn("10.77.0.2")
	for _, n := range []*swarmNode{a, c} {
		if _, err := n.mod.storeEndpoints(b.id, endpointsWithTTL("10.77.0.2")); err != nil {
			t.Fatalf("storeEndpoints: %v", err)
		}
	}
	return
}

// TestEndpointsChangeReachesNodeBehindNAT (criterion 5, the netsim A/B/C move
// in process): B moves to a new subnet. A, behind NAT and with no link to B,
// stores B's new address, pulled from C after C's forward, and emits one
// EndpointsUpdatedEvent for B, within the announcement wait plus 1 s. No row
// reaches A other than by that pull.
// note: the EndpointsUpdatedEvent wakes A's link task for B at once
// (mod/user TestEndpointsUpdatedEventWakesSiblingDuringBackoff), and the woken
// task dials the rows A now holds.
func TestEndpointsChangeReachesNodeBehindNAT(t *testing.T) {
	a, b, c := natSwarm(t)

	start := time.Now()
	b.changeAddress(t, "10.78.0.2")

	eventually(t, "B's new address on A", func() bool { return a.holds(b, "10.78.0.2") })
	if took, bound := time.Since(start), swarmAnnounceDelay+time.Second; took > bound {
		t.Fatalf("A stored B's new address %v after the change; want at most %v", took, bound)
	}
	eventually(t, "an EndpointsUpdatedEvent for B on A", func() bool { return a.updatedFor(b) == 1 })

	for _, p := range a.pulls() {
		if !p.resolver.IsEqual(c.id) || !p.identity.IsEqual(b.id) {
			t.Fatalf("A pulled endpoints of %v from %v; want B's from C alone", p.identity, p.resolver)
		}
	}
	if n := len(a.pulls()); n != 1 {
		t.Fatalf("A pulled %v times; want once", n)
	}
	if !c.holds(b, "10.78.0.2") || c.updatedFor(b) != 1 {
		t.Fatal("C does not hold B's new address from its own pull")
	}
	if n := len(b.announced.list()); n != 1 {
		t.Fatalf("B announced %v times; want once", n)
	}
}

// TestEndpointsDoubleChangeDeliversFinalSet (criterion 6, the netsim double
// change in process): B's address changes twice within the announcement wait,
// the second change at 0.7 of the wait (3.5 s of 5 s in the field). B
// announces once, A and C store B's final address, and neither stores the
// intermediate one, because each pull runs after the wait.
func TestEndpointsDoubleChangeDeliversFinalSet(t *testing.T) {
	a, b, c := natSwarm(t)

	b.changeAddress(t, "10.79.0.2")
	time.Sleep(swarmAnnounceDelay * 7 / 10)
	b.changeAddress(t, "10.80.0.2")

	eventually(t, "B's final address on C", func() bool { return c.holds(b, "10.80.0.2") })
	eventually(t, "B's final address on A", func() bool { return a.holds(b, "10.80.0.2") })
	eventually(t, "an EndpointsUpdatedEvent for B on A", func() bool { return a.updatedFor(b) == 1 })
	time.Sleep(swarmAnnounceDelay + absenceWindow)

	if n := len(b.announced.list()); n != 1 {
		t.Fatalf("B announced %v times; want once for both changes", n)
	}
	for name, n := range map[string]*swarmNode{"A": a, "C": c} {
		if n.holds(b, "10.79.0.2") {
			t.Fatalf("%v stores B's intermediate address; want the final set alone", name)
		}
	}
}
