package user

import (
	"errors"
	"fmt"
	usermod "github.com/astralp2p/astrald/mod/user"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// localPeer reaches another test node's store by direct calls.
type localPeer struct{ n *stateNode }

func (p localPeer) SwarmDigest(*astral.Context) ([]*astral.ObjectID, error) {
	return p.n.store.set.Digest(), nil
}

func (p localPeer) SwarmFetch(_ *astral.Context, ids []*astral.ObjectID) (objs []astral.Object, err error) {
	for _, id := range ids {
		if obj, err := p.n.store.set.Entry(id); err == nil {
			objs = append(objs, obj)
		}
	}

	return objs, nil
}

func (p localPeer) SwarmPush(ctx *astral.Context, obj astral.Object) error {
	return p.n.store.merge(ctx, obj)
}

// orderedSource answers a fixed list of entries in a fixed order, whatever a node stores.
type orderedSource []astral.Object

func (s orderedSource) SwarmDigest(*astral.Context) (ids []*astral.ObjectID, err error) {
	for _, obj := range s {
		id, _ := astral.ResolveObjectID(obj)
		ids = append(ids, id)
	}

	return ids, nil
}

func (s orderedSource) SwarmFetch(*astral.Context, []*astral.ObjectID) ([]astral.Object, error) {
	return s, nil
}

// swarmLink is how the nodes of a test swarm reach each other.
type swarmLink struct {
	name string
	// peer returns how from reaches to.
	peer func(t *testing.T, from, to *stateNode) swarmPeerForTest
}

type swarmPeerForTest interface {
	swarmSource
	swarmEntryPusher
}

// swarmLinks are the two ways: direct calls, and the exchange ops over a router, as the network carries them.
var swarmLinks = []swarmLink{
	{"direct", func(_ *testing.T, _, to *stateNode) swarmPeerForTest { return localPeer{to} }},
	{"ops", func(t *testing.T, from, to *stateNode) swarmPeerForTest {
		return &routerSwarmPeer{router: swarmRouter(t, to, func(*astral.Identity) bool { return true }), caller: from.id, target: to.id}
	}},
}

// swarmRouter routes nodes.<op> queries to the exchange ops over the store of n, as the node router does.
func swarmRouter(t *testing.T, n *stateNode, admit func(*astral.Identity) bool) astral.Router {
	t.Helper()

	ops := routing.NewOpRouter()
	if err := addSwarmOps(ops, n.store, admit); err != nil {
		t.Fatalf("addSwarmOps: %v", err)
	}

	router := routing.NewScopeRouter(nil)
	router.Add(usermod.ModuleName, ops)

	return router
}

// newStateSwarm returns count nodes over one script, each passing its entries on to every other node through link.
func newStateSwarm(t *testing.T, link swarmLink, script *swarmScript, count int) []*stateNode {
	t.Helper()

	nodes := make([]*stateNode, count)
	for i := range nodes {
		nodes[i] = newStateNode(t, script)
	}

	for _, from := range nodes {
		var peers []swarmEntryPusher
		for _, to := range nodes {
			if to != from {
				peers = append(peers, link.peer(t, from, to))
			}
		}
		from.connect(peers...)
	}

	return nodes
}

// pullAll has every node pull from every other node once.
func pullAll(t *testing.T, link swarmLink, nodes []*stateNode) {
	t.Helper()

	for _, to := range nodes {
		for _, from := range nodes {
			if from == to {
				continue
			}
			if _, err := to.store.pullSwarm(to.store.ctx, link.peer(t, to, from)); err != nil {
				t.Fatalf("pull: %v", err)
			}
		}
	}
}

func idStrings(objs ...astral.Object) []string {
	var ids []string
	for _, obj := range objs {
		id, _ := astral.ResolveObjectID(obj)
		ids = append(ids, id.String())
	}
	slices.Sort(ids)

	return slices.Compact(ids)
}

// requireSet fails unless every node stores exactly want and counts it in its head. It waits for the entries a
// store passes on in the background.
func requireSet(t *testing.T, nodes []*stateNode, want []string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		settled := true
		for _, n := range nodes {
			settled = settled && slices.Equal(storedIDs(t, n.store), want)
		}
		if settled {
			break
		}
		time.Sleep(time.Millisecond)
	}

	for i, n := range nodes {
		if got := storedIDs(t, n.store); !slices.Equal(got, want) {
			t.Fatalf("node %v stores %v entries, want %v:\n got %v\nwant %v", i, len(got), len(want), got, want)
		}
		if n.store.set.Head() != uint64(len(want)) {
			t.Fatalf("node %v head = %v, want %v", i, n.store.set.Head(), len(want))
		}
	}
}

// TestSwarmCopiesConvergeToUnion (Done: copies converge to the union): two and three nodes that each stored
// their own entries, one of them shared, end with the union after pulling from each other, and an entry stored
// later on one node reaches every node by being passed on.
func TestSwarmCopiesConvergeToUnion(t *testing.T) {
	for _, link := range swarmLinks {
		for _, count := range []int{2, 3} {
			t.Run(fmt.Sprintf("%v/%v nodes", link.name, count), func(t *testing.T) {
				nodes := newStateSwarm(t, link, newSwarmScript(), count)
				for _, n := range nodes {
					n.store.peers = nil // why: each copy diverges first, without passing on.
				}

				shared := fiveEntries(t)[0]
				var all []astral.Object
				for _, n := range nodes {
					own := fiveEntries(t)
					for _, obj := range append(own, shared) {
						mustAdd(t, n, obj)
					}
					all = append(all, own...)
				}
				all = append(all, shared)

				pullAll(t, link, nodes)
				requireSet(t, nodes, idStrings(all...))

				nodes = newStateSwarm(t, link, newSwarmScript(), count)
				later := fiveEntries(t)
				for i, obj := range later {
					mustAdd(t, nodes[i%count], obj)
				}
				requireSet(t, nodes, idStrings(later...))
			})
		}
	}
}

// TestSwarmDroppedEntryNeverStoredOrForwarded (Done: a dropped entry is never stored or forwarded): an entry
// the check drops, offered locally, by push or by a pull from a source that has it, is stored by no node,
// passed on by no node and listed in no digest, while an entry offered with it reaches every node.
func TestSwarmDroppedEntryNeverStoredOrForwarded(t *testing.T) {
	for _, link := range swarmLinks {
		t.Run(link.name, func(t *testing.T) {
			script := newSwarmScript()
			nodes := newStateSwarm(t, link, script, 3)
			es := fiveEntries(t)
			dropped, good := es[1], es[0]
			droppedID := entryID(t, dropped)
			script.drops[droppedID] = true

			if _, err := nodes[0].store.pullSwarm(nodes[0].store.ctx, orderedSource{dropped, good}); err != nil {
				t.Fatalf("pull: %v", err)
			}
			if err := nodes[1].store.merge(nodes[1].store.ctx, dropped); !errors.Is(err, usermod.ErrSwarmEntryDropped) {
				t.Fatalf("local add: err = %v, want ErrSwarmEntryDropped", err)
			}
			if err := link.peer(t, nodes[1], nodes[2]).SwarmPush(nodes[1].store.ctx, dropped); err == nil {
				t.Fatal("push of a dropped entry answered no error")
			}
			pullAll(t, link, nodes)

			requireSet(t, nodes, idStrings(good))
			for i, n := range nodes {
				if _, err := n.store.set.Entry(&droppedID); err == nil {
					t.Fatalf("node %v reads the dropped entry", i)
				}
				if n.pushed.has(droppedID) {
					t.Fatalf("node %v passed the dropped entry on", i)
				}
				if slices.ContainsFunc(n.store.set.Digest(), func(id *astral.ObjectID) bool { return *id == droppedID }) {
					t.Fatalf("node %v lists the dropped entry in its digest", i)
				}
			}
		})
	}
}

// TestSwarmWaitingEntryAdmitted (Done: a waiting entry is admitted once its gating identity's membership, or
// the request it answers, arrives): an Approval that arrives before its request, and a request that arrives
// before its carrier's membership, are held, unstored and not passed on, are checked again after each merge,
// and are stored and passed on once what they need arrives, on the node they wait on and on every other node.
func TestSwarmWaitingEntryAdmitted(t *testing.T) {
	for _, link := range swarmLinks {
		t.Run(link.name, func(t *testing.T) {
			script := newSwarmScript()
			nodes := newStateSwarm(t, link, script, 3)
			a, b := nodes[0], nodes[1]

			carrier, joiner := astral.GenerateIdentity(), astral.GenerateIdentity()
			membership := membershipEntry(t, astral.GenerateIdentity(), carrier)
			request := joinRequestEntry(joiner, carrier)
			approval := approvalEntry(t, astral.GenerateIdentity(), request)
			script.needs[entryID(t, request)] = entryID(t, membership)
			script.needs[entryID(t, approval)] = entryID(t, request)

			for _, obj := range []astral.Object{approval, request} {
				if err := a.store.merge(a.store.ctx, obj); !errors.Is(err, usermod.ErrSwarmEntryHeld) {
					t.Fatalf("%v: err = %v, want ErrSwarmEntryHeld", obj.ObjectType(), err)
				}
			}
			requireSet(t, nodes, nil)
			if a.pushed.count() != 0 {
				t.Fatalf("a passed on %v held entries", a.pushed.count())
			}

			// note: an unrelated entry is the next merge; both held entries are checked again and still wait.
			unrelated := expelEntry(astral.GenerateIdentity(), astral.GenerateIdentity())
			checks := a.checker.callCount()
			mustAdd(t, a, unrelated)
			if got := a.checker.callCount() - checks; got != 3 {
				t.Fatalf("the next merge made %v checks, want 3: the entry and both held ones", got)
			}
			requireSet(t, nodes, idStrings(unrelated))

			// note: the membership arrives on another node and is passed on to a, which admits both held entries.
			mustAdd(t, b, membership)
			requireSet(t, nodes, idStrings(unrelated, membership, request, approval))
			if !a.pushed.has(entryID(t, request)) || !a.pushed.has(entryID(t, approval)) {
				t.Fatal("a did not pass the admitted entries on")
			}

			// note: a new node pulls them answer first, and admits all of them within the one pull.
			fresh := newStateNode(t, script)
			added, err := fresh.store.pullSwarm(fresh.store.ctx, orderedSource{approval, request, membership, unrelated})
			if err != nil || added != 4 {
				t.Fatalf("pull: added %v, %v; want 4, nil", added, err)
			}
			requireSet(t, []*stateNode{fresh}, idStrings(unrelated, membership, request, approval))
		})
	}
}

// TestSwarmOpsRefuseUnadmittedCaller: the reading ops serve no caller that admit refuses; a push from any
// identified caller is merged and checked like any other entry.
func TestSwarmOpsRefuseUnadmittedCaller(t *testing.T) {
	n, other := newStateNode(t, newSwarmScript()), newStateNode(t, newSwarmScript())
	mustAdd(t, n, fiveEntries(t)[0])

	peer := &routerSwarmPeer{router: swarmRouter(t, n, func(*astral.Identity) bool { return false }), caller: other.id, target: n.id}
	if _, err := peer.SwarmDigest(other.store.ctx); err == nil {
		t.Fatal("digest served a refused caller")
	}
	if err := peer.SwarmPush(other.store.ctx, fiveEntries(t)[1]); err != nil || n.store.set.Head() != 2 {
		t.Fatalf("push from an unadmitted caller: err %v, head %v; want it merged", err, n.store.set.Head())
	}
	if _, err := peer.SwarmDigest(astral.NewContext(nil).LimitZone(astral.ZoneDevice)); !errors.Is(err, astral.ErrZoneExcluded) {
		t.Fatalf("digest without the network zone: err = %v, want ErrZoneExcluded", err)
	}
}

// TestSwarmConcurrentMergesConverge: three nodes that merge and pass on concurrently end with the union.
func TestSwarmConcurrentMergesConverge(t *testing.T) {
	for _, link := range swarmLinks {
		t.Run(link.name, func(t *testing.T) {
			nodes := newStateSwarm(t, link, newSwarmScript(), 3)

			var mu sync.Mutex
			var all []astral.Object
			var wg sync.WaitGroup
			for _, n := range nodes {
				es := append(fiveEntries(t), fiveEntries(t)...)
				mu.Lock()
				all = append(all, es...)
				mu.Unlock()

				wg.Add(1)
				go func() {
					defer wg.Done()
					for _, obj := range es {
						_ = n.store.merge(n.store.ctx, obj)
					}
				}()
			}
			wg.Wait()

			pullAll(t, link, nodes)
			requireSet(t, nodes, idStrings(all...))
		})
	}
}
