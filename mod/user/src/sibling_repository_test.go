package user

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// siblingTestTimeout bounds every wait in these tests, so a broken read fails instead of hanging.
const siblingTestTimeout = 10 * time.Second

// routedQuery is one query the sibling repository routed.
type routedQuery struct {
	ctxID    *astral.Identity
	deadline time.Time
	caller   *astral.Identity
	target   *astral.Identity
	query    string
}

// siblingNode is this node in the sibling repository tests. It routes a query
// to a target listed in answers by streaming that target's bytes, and rejects
// a query to any other target.
//
// why: release holds the accepted bytes until the test closes it, so a test can
// end the Read context before a single byte arrives.
type siblingNode struct {
	id      *astral.Identity
	answers map[string][]byte
	release chan struct{}

	mu    sync.Mutex
	asked []routedQuery
}

func (n *siblingNode) Identity() *astral.Identity { return n.id }

func (n *siblingNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	deadline, _ := ctx.Deadline()

	n.mu.Lock()
	n.asked = append(n.asked, routedQuery{
		ctxID:    ctx.Identity(),
		deadline: deadline,
		caller:   q.Caller,
		target:   q.Target,
		query:    q.QueryString.String(),
	})
	n.mu.Unlock()

	payload, ok := n.answers[q.Target.String()]
	if !ok {
		return query.Reject()
	}

	go func() {
		if n.release != nil {
			<-n.release
		}
		w.Write(payload)
		w.Close()
	}()

	return discardWriter{}, nil
}

func (n *siblingNode) routed() []routedQuery {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]routedQuery(nil), n.asked...)
}

// discardWriter is the sibling's end of an accepted read; the read sends nothing to it.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriter) Close() error                { return nil }

// linkedNodes reports a link to the listed identities alone.
type linkedNodes struct {
	nodesmod.Module
	linked []*astral.Identity
}

func (n *linkedNodes) IsLinked(id *astral.Identity) bool {
	return slicesContainsIdentity(n.linked, id)
}

// siblingFixture builds a claimed node whose swarm holds the given members, of
// which the linked ones have a live link.
func siblingFixture(t *testing.T, members []*astral.Identity, linked []*astral.Identity) (*Module, *siblingNode) {
	t.Helper()

	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()

	contracts := []*auth.SignedContract{adminNetworkMembership(userID, nodeID)}
	for _, m := range members {
		contracts = append(contracts, adminNetworkMembership(userID, m))
	}

	node := &siblingNode{id: nodeID, answers: map[string][]byte{}}

	mod := banModule(t, userID)
	mod.node = node
	mod.log = log.New(nodeID)
	mod.Deps.Auth = &adminNetworkContracts{contracts: contracts}
	mod.Deps.Nodes = &linkedNodes{linked: linked}

	return mod, node
}

// networkContext is the context of a caller that admits the network zone.
func networkContext() *astral.Context {
	return astral.NewContext(nil).WithIdentity(astral.GenerateIdentity()).IncludeZone(astral.ZoneNetwork)
}

// TestSiblingRepositoryReadsTheDeviceRepositoryAsThisNode: a read asks the
// linked sibling for the device repository, under this node's identity and
// within the routing timeout, and the stream it returns outlives the Read
// context.
func TestSiblingRepositoryReadsTheDeviceRepositoryAsThisNode(t *testing.T) {
	sibling := astral.GenerateIdentity()
	mod, node := siblingFixture(t, []*astral.Identity{sibling}, []*astral.Identity{sibling})
	node.answers[sibling.String()] = []byte("sibling bytes")
	node.release = make(chan struct{})

	repo := &SiblingRepository{mod: mod}
	id := testObjectID()

	ctx, cancel := networkContext().WithCancel()
	r, err := repo.Read(ctx, id, 3, 7)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer r.Close()
	returnedAt := time.Now()

	// note: readConcurrent ends the Read context as soon as Read returns.
	cancel()
	close(node.release)

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the stream after the Read context ended: %v", err)
	}
	if string(got) != "sibling bytes" {
		t.Fatalf("read %q; want the sibling's bytes", got)
	}

	if !r.ID().IsEqual(id) {
		t.Fatalf("reader reports %v; want %v", r.ID(), id)
	}
	if r.Repo() != repo {
		t.Fatalf("reader reports repository %v; want the sibling repository", r.Repo())
	}

	routed := node.routed()
	if len(routed) != 1 {
		t.Fatalf("routed %d queries; want 1", len(routed))
	}
	q := routed[0]

	if !q.caller.IsEqual(node.id) || !q.ctxID.IsEqual(node.id) {
		t.Fatalf("routed as caller %v in context %v; want this node %v", q.caller, q.ctxID, node.id)
	}
	if !q.target.IsEqual(sibling) {
		t.Fatalf("routed to %v; want the sibling %v", q.target, sibling)
	}
	if q.deadline.IsZero() || q.deadline.After(returnedAt.Add(siblingReadTimeout)) {
		t.Fatalf("routed with deadline %v; want one within %v of %v", q.deadline, siblingReadTimeout, returnedAt)
	}

	path, params := query.Parse(q.query)
	want := map[string]string{"id": id.String(), "offset": "3", "limit": "7", "repo": objects.RepoDevice}
	if path != objects.MethodRead {
		t.Fatalf("routed %q; want %q", path, objects.MethodRead)
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("routed %s=%q; want %q", k, params[k], v)
		}
	}
}

// TestSiblingRepositoryAsksTheNextSiblingAfterARefusal: a refusal is a miss, and
// the next linked sibling is asked.
func TestSiblingRepositoryAsksTheNextSiblingAfterARefusal(t *testing.T) {
	refusing, holding := astral.GenerateIdentity(), astral.GenerateIdentity()
	siblings := []*astral.Identity{refusing, holding}
	mod, node := siblingFixture(t, siblings, siblings)
	node.answers[holding.String()] = []byte("held")

	r, err := (&SiblingRepository{mod: mod}).Read(networkContext(), testObjectID(), 0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil || string(got) != "held" {
		t.Fatalf("read %q, %v; want the holding sibling's bytes", got, err)
	}

	routed := node.routed()
	if len(routed) != 2 || !routed[0].target.IsEqual(refusing) || !routed[1].target.IsEqual(holding) {
		t.Fatalf("routed %d queries; want the refusing sibling, then the holding one", len(routed))
	}
}

// TestSiblingRepositoryMissesWithoutAnAnswer covers every read that ends in
// ErrNotFound, and the one that ends in ErrHashLookupUnsupported. Only the
// read that every linked sibling refuses routes a query.
func TestSiblingRepositoryMissesWithoutAnAnswer(t *testing.T) {
	member := astral.GenerateIdentity()
	partial := &astral.ObjectID{Hash: testObjectID().Hash}

	cases := []struct {
		name    string
		linked  []*astral.Identity
		ctx     *astral.Context
		id      *astral.ObjectID
		want    error
		queries int
	}{
		{"no linked sibling", nil, networkContext(), testObjectID(), objectsmod.ErrNotFound, 0},
		{"a context without the network zone", []*astral.Identity{member}, networkContext().ExcludeZone(astral.ZoneNetwork), testObjectID(), objectsmod.ErrNotFound, 0},
		{"a partial id", []*astral.Identity{member}, networkContext(), partial, objectsmod.ErrHashLookupUnsupported, 0},
		{"every sibling refuses", []*astral.Identity{member}, networkContext(), testObjectID(), objectsmod.ErrNotFound, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod, node := siblingFixture(t, []*astral.Identity{member}, c.linked)

			startedAt := time.Now()
			r, err := (&SiblingRepository{mod: mod}).Read(c.ctx, c.id, 0, 0)
			if r != nil || !errors.Is(err, c.want) {
				t.Fatalf("read answered %v, %v; want %v", r, err, c.want)
			}
			if d := time.Since(startedAt); d > time.Second {
				t.Fatalf("read took %v to miss; want no wait", d)
			}
			if n := len(node.routed()); n != c.queries {
				t.Fatalf("routed %d queries; want %d", n, c.queries)
			}
		})
	}
}

// TestSiblingRepositoryHoldsNothing: Contains misses, Scan emits no object, and
// a following Scan emits the snapshot boundary and closes when its context ends.
func TestSiblingRepositoryHoldsNothing(t *testing.T) {
	repo := &SiblingRepository{}
	ctx := networkContext()

	if has, err := repo.Contains(ctx, testObjectID()); has || err != nil {
		t.Fatalf("contains answered %v, %v; want false, nil", has, err)
	}

	ch, err := repo.Scan(ctx, false)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if id, ok := <-ch; ok {
		t.Fatalf("scan emitted %v; want a closed channel", id)
	}

	fctx, cancel := ctx.WithCancel()
	ch, err = repo.Scan(fctx, true)
	if err != nil {
		t.Fatalf("scan with follow: %v", err)
	}
	if id, ok := <-ch; !ok || id != nil {
		t.Fatalf("scan with follow emitted %v, %v; want the nil boundary", id, ok)
	}

	cancel()
	select {
	case id, ok := <-ch:
		if ok {
			t.Fatalf("scan with follow emitted %v after the boundary; want a close", id)
		}
	case <-time.After(siblingTestTimeout):
		t.Fatal("scan with follow stayed open after its context ended")
	}

	if _, err := repo.Create(ctx, nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("create answered %v; want ErrUnsupported", err)
	}
	if err := repo.Delete(ctx, testObjectID()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("delete answered %v; want ErrUnsupported", err)
	}
}
