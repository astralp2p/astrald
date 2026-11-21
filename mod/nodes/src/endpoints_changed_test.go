package nodes

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testAnnounceDelay replaces the 5 s announcement wait in tests.
const testAnnounceDelay = 300 * time.Millisecond

// push is one object pushed to a target.
type push struct {
	target *astral.Identity
	object astral.Object
}

// pushLog records pushes in order.
type pushLog struct {
	mu     sync.Mutex
	pushes []push
}

func (l *pushLog) add(p push) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pushes = append(l.pushes, p)
}

func (l *pushLog) list() []push {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]push(nil), l.pushes...)
}

// swarmUser is a user module with a fixed local swarm that records PushToSiblings calls.
type swarmUser struct {
	usermod.Module
	swarm    []*astral.Identity
	siblings pushLog
}

func (u *swarmUser) LocalSwarm() []*astral.Identity { return u.swarm }

func (u *swarmUser) PushToSiblings(_ *astral.Context, obj astral.Object) {
	u.siblings.add(push{object: obj})
}

// pushObjects is an objects module that records every push.
type pushObjects struct {
	objectsmod.Module
	pushes pushLog
}

func (o *pushObjects) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	o.pushes.add(push{target: target, object: obj})
	return nil
}

// pull is one remote resolve call.
type pull struct {
	resolver *astral.Identity
	identity *astral.Identity
}

// exchangeFixture is a nodes module with a database, a fixed local swarm, fake
// pushes and a fake remote resolver.
type exchangeFixture struct {
	mod     *Module
	self    *astral.Identity
	user    *swarmUser
	objects *pushObjects
	events  *recordingEvents
	pulls   chan pull
	// answer is what the fake remote resolver returns.
	answer []*nodes.EndpointWithTTL
	// own is the resolver that answers for this node's own endpoint set.
	own *ownResolver
}

// ownResolver resolves a settable endpoint set.
type ownResolver struct {
	mu   sync.Mutex
	rows []*nodes.EndpointWithTTL
}

func (r *ownResolver) set(rows ...*nodes.EndpointWithTTL) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = rows
}

func (r *ownResolver) ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sig.ArrayToChan(append([]*nodes.EndpointWithTTL(nil), r.rows...)), nil
}

func newExchangeFixture(t *testing.T) *exchangeFixture {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f := &exchangeFixture{
		self:    astral.GenerateIdentity(),
		objects: &pushObjects{},
		events:  &recordingEvents{},
		pulls:   make(chan pull, 16),
		own:     &ownResolver{},
	}
	f.user = &swarmUser{swarm: []*astral.Identity{f.self}}

	f.mod = &Module{
		Deps:          Deps{Events: f.events, User: f.user, Objects: f.objects},
		node:          &identityNode{id: f.self},
		log:           log.New(f.self),
		ctx:           ctx,
		cancel:        cancel,
		db:            testEndpointDB(t),
		announceDelay: testAnnounceDelay,
	}
	f.mod.resolveRemote = func(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		f.pulls <- pull{resolver: resolver, identity: identity}
		return f.answer, nil
	}
	f.mod.linkPool = NewLinkPool(f.mod)
	f.mod.resolvers.Add(f.own)

	return f
}

// testEndpointDB opens an endpoint table in a fresh SQLite file.
func testEndpointDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "nodes.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{gdb}
	if err := db.AutoMigrate(&dbEndpoint{}, &dbEndpointHealth{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// member adds a local swarm member, linked when linked is set.
func (f *exchangeFixture) member(linked bool) *astral.Identity {
	id := astral.GenerateIdentity()
	f.user.swarm = append(f.user.swarm, id)
	if linked {
		link := &Link{id: astral.NewNonce(), conn: &identityConn{remote: id}}
		f.mod.linkPool.links.Add(link)
	}
	return id
}

// stranger adds a linked node outside the local swarm.
func (f *exchangeFixture) stranger() *astral.Identity {
	id := astral.GenerateIdentity()
	f.mod.linkPool.links.Add(&Link{id: astral.NewNonce(), conn: &identityConn{remote: id}})
	return id
}

// requirePull waits for one remote resolve call and checks its arguments.
func (f *exchangeFixture) requirePull(t *testing.T, resolver, identity *astral.Identity) {
	t.Helper()
	select {
	case p := <-f.pulls:
		if !p.resolver.IsEqual(resolver) || !p.identity.IsEqual(identity) {
			t.Fatalf("pulled endpoints of %v from %v; want of %v from %v", p.identity, p.resolver, identity, resolver)
		}
	case <-time.After(effectTimeout):
		t.Fatal("no pull started")
	}
}

// requireNoPull checks that no remote resolve call starts within absenceWindow.
func (f *exchangeFixture) requireNoPull(t *testing.T) {
	t.Helper()
	select {
	case p := <-f.pulls:
		t.Fatalf("pulled endpoints of %v from %v; want no pull", p.identity, p.resolver)
	case <-time.After(absenceWindow):
	}
}

// eventually polls cond until it holds or effectTimeout passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(effectTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%v did not happen within %v", what, effectTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func addressChanged(source *astral.Identity) *events.Event {
	return addressChangedTo(source, "203.0.113.7")
}

func addressChangedTo(source *astral.Identity, added string) *events.Event {
	return &events.Event{
		ID:       astral.NewNonce(),
		SourceID: source,
		Data:     &ip.EventNetworkAddressChanged{Added: []ip.IP{tcpEndpoint(added).IP}},
	}
}

func endpointsWithTTL(addrs ...string) (list []*nodes.EndpointWithTTL) {
	for _, a := range addrs {
		list = append(list, nodes.NewEndpointWithTTL(tcpEndpoint(a), time.Hour))
	}
	return
}

// TestAnnounceEndpointsDebounced (criterion 1): an address change of this node
// pushes one EndpointsChangedMessage about this node to the siblings after the
// wait; three changes within the wait push one; a forged event and an IPv6
// link-local change push none.
func TestAnnounceEndpointsDebounced(t *testing.T) {
	cases := []struct {
		name    string
		changes int
		local   bool
		added   string
		pushes  int
	}{
		{"one change", 1, true, "203.0.113.7", 1},
		{"three changes within the wait", 3, true, "203.0.113.7", 1},
		{"a change event from another node", 1, false, "203.0.113.7", 0},
		{"an IPv6 link-local change", 1, true, "fe80::1", 0},
		{"an IPv4 link-local change", 1, true, "169.254.10.1", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExchangeFixture(t)
			sender := f.self
			if !tc.local {
				sender = f.member(true)
			}

			for i := 0; i < tc.changes; i++ {
				drop := &recordingDrop{sender: sender, object: addressChangedTo(f.self, tc.added)}
				if err := f.mod.ReceiveObject(drop); err != nil {
					t.Fatalf("ReceiveObject: %v", err)
				}
				time.Sleep(testAnnounceDelay / 10)
			}

			if n := len(f.user.siblings.list()); n != 0 {
				t.Fatalf("pushed %v announcements before the wait ended; want none", n)
			}

			time.Sleep(testAnnounceDelay + absenceWindow)
			pushes := f.user.siblings.list()
			if len(pushes) != tc.pushes {
				t.Fatalf("pushed %v announcements; want %v", len(pushes), tc.pushes)
			}
			for _, p := range pushes {
				m, ok := p.object.(*nodes.EndpointsChangedMessage)
				if !ok || !m.Subject.IsEqual(f.self) {
					t.Fatalf("pushed %v; want an EndpointsChangedMessage about this node", p.object)
				}
			}
		})
	}
}

// TestAnnounceEndpointsAfterWaitStartsAnew: a change after an announcement
// starts a second wait and a second announcement.
func TestAnnounceEndpointsAfterWaitStartsAnew(t *testing.T) {
	f := newExchangeFixture(t)

	for i := 1; i <= 2; i++ {
		// note: the set must differ, because an announcement is gated by the fingerprint.
		f.own.set(endpointsWithTTL(fmt.Sprintf("198.51.100.%d", i))...)
		if err := f.mod.ReceiveObject(&recordingDrop{sender: f.self, object: addressChanged(f.self)}); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}
		eventually(t, "announcement", func() bool { return len(f.user.siblings.list()) == i })
	}
}

// announced returns how many announcements this node pushed to its siblings.
func (f *exchangeFixture) announced() int { return len(f.user.siblings.list()) }

// baseline runs the first check, which announces once whatever the set holds,
// and returns the set it announced.
func (f *exchangeFixture) baseline(t *testing.T, rows ...*nodes.EndpointWithTTL) {
	t.Helper()
	f.own.set(rows...)
	f.mod.scheduleEndpointsAnnounce()
	eventually(t, "first announcement", func() bool { return f.announced() == 1 })
	time.Sleep(absenceWindow)
	if n := f.announced(); n != 1 {
		t.Fatalf("pushed %v announcements for the first check; want 1", n)
	}
}

// sendLocal delivers an event of this node to ReceiveObject.
func (f *exchangeFixture) sendLocal(t *testing.T, data astral.Object) {
	t.Helper()
	event := &events.Event{ID: astral.NewNonce(), SourceID: f.self, Data: data}
	if err := f.mod.ReceiveObject(&recordingDrop{sender: f.self, object: event}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
}

// waitOut sleeps past the announcement wait.
func waitOut() { time.Sleep(testAnnounceDelay + absenceWindow) }

func gatewayRow(t *testing.T) *nodes.EndpointWithTTL {
	t.Helper()
	return nodes.NewEndpointWithTTL(gateway.NewEndpoint(newTestIdentity(t), astral.GenerateIdentity()), time.Hour)
}

// TestAnnounceOnGatewayAddedLinkCreated (criterion 1): a gateway row appears
// without an event and the next link-created event pushes exactly one
// announcement about this node after the wait.
func TestAnnounceOnGatewayAddedLinkCreated(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t, endpointsWithTTL("198.51.100.1")...)

	f.own.set(append(endpointsWithTTL("198.51.100.1"), gatewayRow(t))...)
	f.sendLocal(t, &nodes.LinkCreatedEvent{RemoteIdentity: astral.GenerateIdentity(), LinkCount: 2})

	if n := f.announced(); n != 1 {
		t.Fatalf("pushed %v announcements before the wait ended; want none new", n)
	}
	eventually(t, "announcement", func() bool { return f.announced() == 2 })
	time.Sleep(absenceWindow)

	pushes := f.user.siblings.list()
	if len(pushes) != 2 {
		t.Fatalf("pushed %v announcements; want 2 (first check, gateway added)", len(pushes))
	}
	if m, ok := pushes[1].object.(*nodes.EndpointsChangedMessage); !ok || !m.Subject.IsEqual(f.self) {
		t.Fatalf("pushed %v; want an EndpointsChangedMessage about this node", pushes[1].object)
	}
}

// TestAnnounceOnGatewayRemovedLinkClosed (criterion 2): a gateway row
// disappears and the next link-closed event pushes exactly one announcement.
func TestAnnounceOnGatewayRemovedLinkClosed(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t, append(endpointsWithTTL("198.51.100.1"), gatewayRow(t))...)

	f.own.set(endpointsWithTTL("198.51.100.1")...)
	// note: a closed link to a node outside the swarm, with no link left, pulls nothing.
	f.sendLocal(t, &nodes.LinkClosedEvent{RemoteIdentity: astral.GenerateIdentity(), LinkCount: 0})

	eventually(t, "announcement", func() bool { return f.announced() == 2 })
	time.Sleep(absenceWindow)
	if n := f.announced(); n != 2 {
		t.Fatalf("pushed %v announcements; want 2 (first check, gateway removed)", n)
	}
}

// TestNoAnnounceWhenSetUnchanged (criterion 3): an address change event and
// link events push nothing while the resolved set stays the same.
func TestNoAnnounceWhenSetUnchanged(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t, endpointsWithTTL("198.51.100.1")...)

	remote := astral.GenerateIdentity()
	f.sendLocal(t, &ip.EventNetworkAddressChanged{Added: []ip.IP{tcpEndpoint("203.0.113.7").IP}})
	f.sendLocal(t, &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: 1})
	f.sendLocal(t, &nodes.LinkClosedEvent{RemoteIdentity: remote, LinkCount: 0})
	waitOut()

	if n := f.announced(); n != 1 {
		t.Fatalf("pushed %v announcements; want only the first check's", n)
	}
}

// TestOwnEndpointsHashIgnoresOrderAndTTL (criterion 4): the same endpoints in
// another order, with other TTLs or repeated, hash alike; another set does not.
func TestOwnEndpointsHashIgnoresOrderAndTTL(t *testing.T) {
	f := newExchangeFixture(t)
	hash := func(rows ...*nodes.EndpointWithTTL) endpointsHash {
		t.Helper()
		f.own.set(rows...)
		h, err := f.mod.ownEndpointsHash()
		if err != nil {
			t.Fatalf("ownEndpointsHash: %v", err)
		}
		return h
	}

	a, b := tcpEndpoint("198.51.100.1"), tcpEndpoint("198.51.100.2")

	base := hash(nodes.NewEndpointWithTTL(a, time.Hour), nodes.NewEndpointWithTTL(b, time.Hour))
	if got := hash(nodes.NewEndpointWithTTL(b, time.Hour), nodes.NewEndpointWithTTL(a, time.Hour)); got != base {
		t.Fatal("reordered set changed the hash")
	}
	if got := hash(nodes.NewEndpointWithTTL(a, time.Minute), nodes.NewEndpointWithTTL(b)); got != base {
		t.Fatal("changed TTLs changed the hash")
	}
	if got := hash(nodes.NewEndpointWithTTL(a), nodes.NewEndpointWithTTL(b), nodes.NewEndpointWithTTL(a)); got != base {
		t.Fatal("a repeated endpoint changed the hash")
	}
	if got := hash(nodes.NewEndpointWithTTL(a)); got == base {
		t.Fatal("a smaller set kept the hash")
	}
	if got := hash(nodes.NewEndpointWithTTL(a), nodes.NewEndpointWithTTL(b), gatewayRow(t)); got == base {
		t.Fatal("a larger set kept the hash")
	}
}

// TestAnnounceBurstOnePush (criterion 5): several events and a set change
// inside one wait push exactly one announcement.
func TestAnnounceBurstOnePush(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t, endpointsWithTTL("198.51.100.1")...)

	remote := astral.GenerateIdentity()
	f.sendLocal(t, &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: 2})
	f.own.set(append(endpointsWithTTL("198.51.100.1"), gatewayRow(t))...)
	f.sendLocal(t, &nodes.LinkClosedEvent{RemoteIdentity: remote, LinkCount: 0})
	f.sendLocal(t, &ip.EventNetworkAddressChanged{Added: []ip.IP{tcpEndpoint("203.0.113.7").IP}})
	f.sendLocal(t, &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: 2})
	waitOut()

	if n := f.announced(); n != 2 {
		t.Fatalf("pushed %v announcements; want 2 (first check, one for the burst)", n)
	}
}

// TestAnnounceCheckEntryPointOnionThenNoop (criterion 6): an onion endpoint
// appears with no event; the entry point EP4 calls pushes once, and a second
// call pushes nothing.
func TestAnnounceCheckEntryPointOnionThenNoop(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t, endpointsWithTTL("198.51.100.1")...)

	f.own.set(append(endpointsWithTTL("198.51.100.1"), nodes.NewEndpointWithTTL(&tor.Endpoint{Port: 80}, time.Hour))...)
	f.mod.scheduleEndpointsAnnounce()
	eventually(t, "announcement", func() bool { return f.announced() == 2 })
	waitOut()
	if n := f.announced(); n != 2 {
		t.Fatalf("pushed %v announcements after the first call; want 2", n)
	}

	f.mod.scheduleEndpointsAnnounce()
	waitOut()
	if n := f.announced(); n != 2 {
		t.Fatalf("pushed %v announcements after the second call; want 2", n)
	}
}

// TestAnnounceFirstCheckAlwaysAnnounces: the first check after start announces
// even when the set is empty, and a second check of the same set does not.
func TestAnnounceFirstCheckAlwaysAnnounces(t *testing.T) {
	f := newExchangeFixture(t)
	f.baseline(t)

	f.mod.scheduleEndpointsAnnounce()
	waitOut()
	if n := f.announced(); n != 1 {
		t.Fatalf("pushed %v announcements; want 1", n)
	}
}

// TestReceiveEndpointsChangedFirstHand (criterion 2): a notice from a linked
// member about itself pulls from it, emits one EndpointsUpdatedEvent for 2 new
// rows, and forwards the notice to the other linked member alone.
func TestReceiveEndpointsChangedFirstHand(t *testing.T) {
	f := newExchangeFixture(t)
	mover := f.member(true)
	other := f.member(true)
	f.member(false) // note: an unlinked member gets no forward.
	f.answer = endpointsWithTTL("198.51.100.1", "198.51.100.2")

	msg := &nodes.EndpointsChangedMessage{Subject: mover}
	drop := &recordingDrop{sender: mover, object: msg}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
	if a := drop.accepted(); len(a) != 1 || a[0] {
		t.Fatalf("Accept calls %v; want one Accept(false)", a)
	}

	f.requirePull(t, mover, mover)
	eventually(t, "forward", func() bool { return len(f.objects.pushes.list()) > 0 })
	time.Sleep(absenceWindow)

	forwards := f.objects.pushes.list()
	if len(forwards) != 1 || !forwards[0].target.IsEqual(other) || forwards[0].object != msg {
		t.Fatalf("forwards %v; want the notice to %v alone", forwards, other)
	}

	emitted := f.events.recorded()
	if len(emitted) != 1 {
		t.Fatalf("emitted %v events; want one EndpointsUpdatedEvent", len(emitted))
	}
	if e, ok := emitted[0].(*nodes.EndpointsUpdatedEvent); !ok || !e.Identity.IsEqual(mover) {
		t.Fatalf("emitted %v; want an EndpointsUpdatedEvent for %v", emitted[0], mover)
	}

	rows, err := f.mod.db.FindEndpoints(mover)
	if err != nil || len(rows) != 2 {
		t.Fatalf("stored %v rows (err %v); want 2", len(rows), err)
	}
}

// TestReceiveEndpointsChangedForwarded (criterion 3): a forwarded notice
// (sender is not the subject) pulls from the sender and emits the event, but is
// not forwarded again.
func TestReceiveEndpointsChangedForwarded(t *testing.T) {
	f := newExchangeFixture(t)
	relay := f.member(true)
	mover := f.member(true)
	f.member(true)
	f.answer = endpointsWithTTL("198.51.100.1")

	drop := &recordingDrop{sender: relay, object: &nodes.EndpointsChangedMessage{Subject: mover}}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	f.requirePull(t, relay, mover)
	eventually(t, "EndpointsUpdatedEvent", func() bool { return len(f.events.recorded()) == 1 })
	time.Sleep(absenceWindow)

	if forwards := f.objects.pushes.list(); len(forwards) != 0 {
		t.Fatalf("forwarded a forwarded notice %v times; want never", len(forwards))
	}
}

// TestReceiveEndpointsChangedNoNewRows: a first-hand notice whose pull only
// refreshes known rows emits no event and is still forwarded, because the
// receiver may have pulled the new rows on its first link to the subject.
func TestReceiveEndpointsChangedNoNewRows(t *testing.T) {
	f := newExchangeFixture(t)
	mover := f.member(true)
	other := f.member(true)
	f.answer = endpointsWithTTL("198.51.100.1")
	if _, err := f.mod.storeEndpoints(mover, f.answer, false); err != nil {
		t.Fatalf("storeEndpoints: %v", err)
	}

	drop := &recordingDrop{sender: mover, object: &nodes.EndpointsChangedMessage{Subject: mover}}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	f.requirePull(t, mover, mover)
	eventually(t, "forward", func() bool { return len(f.objects.pushes.list()) == 1 })
	time.Sleep(absenceWindow)

	if forwards := f.objects.pushes.list(); len(forwards) != 1 || !forwards[0].target.IsEqual(other) {
		t.Fatalf("forwards %v; want one to %v", forwards, other)
	}
	if emitted := f.events.recorded(); len(emitted) != 0 {
		t.Fatalf("emitted %v events; want none for refreshed rows", len(emitted))
	}
}

// TestReceiveEndpointsChangedRejected (criterion 3): a notice from a sender that
// is not a linked member, or about a subject that is not another member, is
// rejected with ErrPushRejected and starts no pull.
func TestReceiveEndpointsChangedRejected(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *exchangeFixture) (sender, subject *astral.Identity)
	}{
		{"from a linked non-member about itself", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			s := f.stranger()
			return s, s
		}},
		{"from a linked non-member about a member", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return f.stranger(), f.member(true)
		}},
		{"from an unlinked member", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			s := f.member(false)
			return s, s
		}},
		{"from a zero sender", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return nil, f.member(true)
		}},
		{"from this node", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return f.self, f.member(true)
		}},
		{"about a zero subject", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return f.member(true), nil
		}},
		{"about a non-member", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return f.member(true), astral.GenerateIdentity()
		}},
		{"about this node", func(f *exchangeFixture) (*astral.Identity, *astral.Identity) {
			return f.member(true), f.self
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExchangeFixture(t)
			f.member(true) // note: a linked member that a forward would reach.
			sender, subject := tc.setup(f)
			msg := &nodes.EndpointsChangedMessage{Subject: subject}

			if err := f.mod.receiveEndpointsChanged(sender, msg); !errors.Is(err, objectsmod.ErrPushRejected) {
				t.Fatalf("receiveEndpointsChanged = %v; want %v", err, objectsmod.ErrPushRejected)
			}

			drop := &recordingDrop{sender: sender, object: msg}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}
			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; want none", a)
			}

			f.requireNoPull(t)
			if n := len(f.objects.pushes.list()); n != 0 {
				t.Fatalf("forwarded %v times; want never", n)
			}
		})
	}
}

// TestStoreEndpointsCountsNewRows (criterion 2, the count): a live row the pull
// refreshes counts 0, a row the node did not hold counts 1, and so does a row
// whose stored copy has expired.
func TestStoreEndpointsCountsNewRows(t *testing.T) {
	f := newExchangeFixture(t)
	node := astral.GenerateIdentity()

	live := endpointsWithTTL("198.51.100.1")
	if n, err := f.mod.storeEndpoints(node, live, false); err != nil || n != 1 {
		t.Fatalf("first store = %v, %v; want 1 new row", n, err)
	}

	expired := tcpEndpoint("198.51.100.3")
	past := time.Now().UTC().Add(-time.Minute)
	if err := f.mod.db.AddEndpoint(node, expired.Network(), expired.Address(), &past); err != nil {
		t.Fatalf("AddEndpoint: %v", err)
	}

	n, err := f.mod.storeEndpoints(node, endpointsWithTTL("198.51.100.1", "198.51.100.2", "198.51.100.3"), false)
	if err != nil || n != 2 {
		t.Fatalf("second store = %v, %v; want 2 new rows (one unknown, one expired)", n, err)
	}

	if n, err := f.mod.storeEndpoints(node, endpointsWithTTL("198.51.100.2", "198.51.100.2"), false); err != nil || n != 0 {
		t.Fatalf("third store = %v, %v; want 0 new rows", n, err)
	}
}
