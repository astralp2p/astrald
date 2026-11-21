package nodes

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// recordingPushes is an objects module that logs every push before it reaches the target.
type recordingPushes struct {
	*swarmNodeObjects
	log pushLog
}

func (o *recordingPushes) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	o.log.add(push{target: target, object: obj})
	return o.swarmNodeObjects.Push(ctx, target, obj)
}

// recordPushes makes n log its pushes and returns the log.
func recordPushes(n *swarmNode) *pushLog {
	rec := &recordingPushes{swarmNodeObjects: n.mod.Objects.(*swarmNodeObjects)}
	n.mod.Objects = rec
	return &rec.log
}

// firstHandSwarm builds the EP5 lab: A linked to its siblings B and C and to
// the new member D; B and C linked to each other, so a second forward would
// reach a sibling that already pulled. D serves its address for itself.
func firstHandSwarm(t *testing.T) (a, b, c, d *swarmNode) {
	t.Helper()
	s := newTestSwarm(t, 4)
	a, b, c, d = s.nodes[0], s.nodes[1], s.nodes[2], s.nodes[3]
	s.link(a, b)
	s.link(a, c)
	s.link(b, c)
	s.link(a, d)
	d.setOwn("10.81.0.4")
	return
}

// pullsOf returns the pulls of n about identity.
func pullsOf(n *swarmNode, identity *astral.Identity) (list []pull) {
	for _, p := range n.pulls() {
		if p.identity.IsEqual(identity) {
			list = append(list, p)
		}
	}
	return
}

// requireForwardedOnce checks that A forwarded D's notice to B and C alone, and
// that B and C each pulled D once, from A, and forwarded nothing.
func requireForwardedOnce(t *testing.T, sent *pushLog, a, b, c, d *swarmNode) {
	t.Helper()

	eventually(t, "D's row on B and on C", func() bool { return b.holds(d, "10.81.0.4") && c.holds(d, "10.81.0.4") })
	// why: a second forward from B or C would reach the other and cost it a second pull.
	time.Sleep(swarmAnnounceDelay + absenceWindow)

	targets := map[*astral.Identity]int{}
	for _, p := range sent.list() {
		msg, ok := p.object.(*nodes.EndpointsChangedMessage)
		if !ok || !msg.Subject.IsEqual(d.id) {
			t.Fatalf("A pushed %v to %v; want EndpointsChangedMessage{Subject:D}", p.object, p.target)
		}
		targets[p.target]++
	}
	if len(sent.list()) != 2 || targets[b.id] != 1 || targets[c.id] != 1 {
		t.Fatalf("A pushed %v; want one notice to B and one to C, none to D", sent.list())
	}

	if got := pullsOf(a, d.id); len(got) != 1 || !got[0].resolver.IsEqual(d.id) {
		t.Fatalf("A pulled D %v; want once, from D", got)
	}
	for name, n := range map[string]*swarmNode{"B": b, "C": c} {
		if got := pullsOf(n, d.id); len(got) != 1 || !got[0].resolver.IsEqual(a.id) {
			t.Fatalf("%v pulled D %v; want once, from A (a forwarded notice is not forwarded again)", name, got)
		}
		if n.updatedFor(d) != 1 {
			t.Fatalf("%v emitted %v EndpointsUpdatedEvents for D; want one", name, n.updatedFor(d))
		}
	}
	// note: D may pull A's endpoints after A's own announcement of the new link; it must not pull itself.
	if got := pullsOf(d, d.id); len(got) != 0 {
		t.Fatalf("D pulled itself %v; want no notice about D to D", got)
	}
}

// TestRefreshMemberEndpointsForwardsToLinkedSiblings (EP5 criterion 1, the
// nodes half, and criterion 4): RefreshMemberEndpoints(D) on A pulls D from D,
// forwards one notice to each linked sibling and none to D, and the siblings
// pull from A without forwarding again.
func TestRefreshMemberEndpointsForwardsToLinkedSiblings(t *testing.T) {
	a, b, c, d := firstHandSwarm(t)
	sent := recordPushes(a)

	a.mod.RefreshMemberEndpoints(d.id)

	if !a.holds(d, "10.81.0.4") {
		t.Fatal("A holds no row of D after the first-hand pull")
	}
	requireForwardedOnce(t, sent, a, b, c, d)
}

// linkCreated is the event a node emits for its own new link.
func linkCreated(self, remote *astral.Identity, count int) *events.Event {
	return &events.Event{
		ID:       astral.NewNonce(),
		SourceID: self,
		Data:     &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: astral.Uint32(count)},
	}
}

// TestFirstLinkOfMemberForwardsNotice (EP5 criterion 3): the first link to a
// member D makes A pull D from D and forward one notice to each other linked
// sibling; a second link to D does nothing new.
func TestFirstLinkOfMemberForwardsNotice(t *testing.T) {
	a, b, c, d := firstHandSwarm(t)
	sent := recordPushes(a)

	err := a.mod.ReceiveObject(&recordingDrop{sender: a.id, object: linkCreated(a.id, d.id, 1)})
	if err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	eventually(t, "D's row on A", func() bool { return a.holds(d, "10.81.0.4") })
	requireForwardedOnce(t, sent, a, b, c, d)
}

// TestSecondLinkOfMemberDoesNothingNew is the LinkCount==2 half of criterion 3.
func TestSecondLinkOfMemberDoesNothingNew(t *testing.T) {
	a, b, c, d := firstHandSwarm(t)
	sent := recordPushes(a)

	err := a.mod.ReceiveObject(&recordingDrop{sender: a.id, object: linkCreated(a.id, d.id, 2)})
	if err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
	time.Sleep(swarmAnnounceDelay + absenceWindow)

	if got := sent.list(); len(got) != 0 {
		t.Fatalf("A pushed %v; want nothing for a second link", got)
	}
	for name, n := range map[string]*swarmNode{"A": a, "B": b, "C": c} {
		if got := pullsOf(n, d.id); len(got) != 0 {
			t.Fatalf("%v pulled D %v; want no pull for a second link", name, got)
		}
	}
}
