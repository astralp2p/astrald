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
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// swarmRouter routes user.<op> queries to the exchange ops of a module over the store of n, as the node router
// does. The module admits members and the root key, as the running node does.
func swarmRouter(t *testing.T, n *stateNode, members ...*astral.Identity) astral.Router {
	t.Helper()

	mod := &Module{node: &identityNode{id: n.id}, swarmState: n.store}
	useSwarm(t, mod, astral.GenerateIdentity(), members...)
	mod.router.AddStructPrefix(mod, "Op")

	router := routing.NewScopeRouter(nil)
	router.Add(usermod.ModuleName, &mod.router)

	return router
}

// swarmPeerOf returns how from reaches to: the exchange ops over a router, as the network carries them.
func swarmPeerOf(t *testing.T, from, to *stateNode) *routerSwarmPeer {
	t.Helper()

	return &routerSwarmPeer{router: swarmRouter(t, to, from.id), caller: from.id, target: to.id}
}

// scriptedSource is a peer that answers a fixed list of entries in a fixed order, whatever a node stores.
func scriptedSource(t *testing.T, objs ...astral.Object) *routerSwarmPeer {
	t.Helper()

	digest := func(_ *astral.Context, q *routing.IncomingQuery) error {
		ch := q.Accept()
		defer ch.Close()

		for _, obj := range objs {
			id, _ := astral.ResolveObjectID(obj)
			if err := ch.Send(id); err != nil {
				return err
			}
		}
		return ch.Send(&astral.EOS{})
	}
	fetch := func(ctx *astral.Context, q *routing.IncomingQuery) error {
		ch := q.Accept()
		defer ch.Close()

		err := ch.Switch(func(*astral.ObjectID) error { return nil }, channel.BreakOnEOS, channel.FailOnEOF(), channel.WithContext(ctx))
		if err != nil {
			return err
		}
		for _, obj := range objs {
			if err = ch.Send(obj); err != nil {
				return err
			}
		}
		return ch.Send(&astral.EOS{})
	}

	ops := routing.NewOpRouter()
	for name, fn := range map[string]func(*astral.Context, *routing.IncomingQuery) error{opSwarmDigest: digest, opSwarmFetch: fetch} {
		op, err := routing.NewOp(fn)
		if err != nil {
			t.Fatalf("NewOp: %v", err)
		}
		if err = ops.AddOp(name, op); err != nil {
			t.Fatalf("AddOp: %v", err)
		}
	}
	router := routing.NewScopeRouter(nil)
	router.Add(usermod.ModuleName, ops)

	return &routerSwarmPeer{router: router, caller: astral.GenerateIdentity(), target: astral.GenerateIdentity()}
}

// newStateSwarm returns count nodes over one script, each passing its entries on to every other node through the exchange ops.
func newStateSwarm(t *testing.T, script *swarmScript, count int) []*stateNode {
	t.Helper()

	nodes := make([]*stateNode, count)
	for i := range nodes {
		nodes[i] = newStateNode(t, script)
	}

	for _, from := range nodes {
		var peers []swarmEntryPusher
		for _, to := range nodes {
			if to != from {
				peers = append(peers, swarmPeerOf(t, from, to))
			}
		}
		from.connect(peers...)
	}

	return nodes
}

// pullAll has every node pull from every other node once.
func pullAll(t *testing.T, nodes []*stateNode) {
	t.Helper()

	for _, to := range nodes {
		for _, from := range nodes {
			if from == to {
				continue
			}
			if _, err := to.store.pullSwarm(to.store.ctx, swarmPeerOf(t, to, from)); err != nil {
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
	for _, count := range []int{2, 3} {
		t.Run(fmt.Sprintf("%v nodes", count), func(t *testing.T) {
			nodes := newStateSwarm(t, newSwarmScript(), count)
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

			pullAll(t, nodes)
			requireSet(t, nodes, idStrings(all...))

			nodes = newStateSwarm(t, newSwarmScript(), count)
			later := fiveEntries(t)
			for i, obj := range later {
				mustAdd(t, nodes[i%count], obj)
			}
			requireSet(t, nodes, idStrings(later...))
		})
	}
}

// TestSwarmDroppedEntryNeverStoredOrForwarded (Done: a dropped entry is never stored or forwarded): an entry
// the check drops, offered locally, by push or by a pull from a source that has it, is stored by no node,
// passed on by no node and listed in no digest, while an entry offered with it reaches every node.
func TestSwarmDroppedEntryNeverStoredOrForwarded(t *testing.T) {
	script := newSwarmScript()
	nodes := newStateSwarm(t, script, 3)
	es := fiveEntries(t)
	dropped, good := es[1], es[0]
	droppedID := entryID(t, dropped)
	script.drops[droppedID] = true

	if _, err := nodes[0].store.pullSwarm(nodes[0].store.ctx, scriptedSource(t, dropped, good)); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if err := nodes[1].store.merge(nodes[1].store.ctx, dropped); !errors.Is(err, usermod.ErrSwarmEntryDropped) {
		t.Fatalf("local add: err = %v, want ErrSwarmEntryDropped", err)
	}
	if err := swarmPeerOf(t, nodes[1], nodes[2]).SwarmPush(nodes[1].store.ctx, dropped); err == nil {
		t.Fatal("push of a dropped entry answered no error")
	}
	pullAll(t, nodes)

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
}

// TestSwarmWaitingEntryAdmitted (Done: a waiting entry is admitted once its gating identity's membership, or
// the request it answers, arrives): an Approval that arrives before its request, and a request that arrives
// before its carrier's membership, are held, unstored and not passed on, are checked again after each merge,
// and are stored and passed on once what they need arrives, on the node they wait on and on every other node.
func TestSwarmWaitingEntryAdmitted(t *testing.T) {
	script := newSwarmScript()
	nodes := newStateSwarm(t, script, 3)
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
	added, err := fresh.store.pullSwarm(fresh.store.ctx, scriptedSource(t, approval, request, membership, unrelated))
	if err != nil || added != 4 {
		t.Fatalf("pull: added %v, %v; want 4, nil", added, err)
	}
	requireSet(t, []*stateNode{fresh}, idStrings(unrelated, membership, request, approval))
}

// TestSwarmOpsRefuseUnadmittedCaller: the reading ops serve no caller that admit refuses; a push from any
// identified caller is merged and checked like any other entry.
func TestSwarmOpsRefuseUnadmittedCaller(t *testing.T) {
	n, other := newStateNode(t, newSwarmScript()), newStateNode(t, newSwarmScript())
	mustAdd(t, n, fiveEntries(t)[0])

	peer := &routerSwarmPeer{router: swarmRouter(t, n), caller: other.id, target: n.id}
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
	nodes := newStateSwarm(t, newSwarmScript(), 3)

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

	pullAll(t, nodes)
	requireSet(t, nodes, idStrings(all...))
}
