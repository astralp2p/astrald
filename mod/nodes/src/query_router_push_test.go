package nodes

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	nodesapi "github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/nodes"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// contractHolder is the target node's side of a pushed proof: its index of the
// contracts it holds and the authorizer that admits a relay for an app only when
// it holds that app's contract.
type contractHolder struct {
	authmod.Module
	mu   sync.Mutex
	held map[string]bool // issuer -> held
}

func (h *contractHolder) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	relay, ok := action.(*nodesapi.RelayForAction)
	if !ok {
		return false
	}
	return h.holds(relay.ForID)
}

func (h *contractHolder) holds(issuer *astral.Identity) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held[issuer.String()]
}

// index holds sc; a contract held already stays held, with no error.
func (h *contractHolder) index(sc *auth.SignedContract) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held == nil {
		h.held = map[string]bool{}
	}
	h.held[sc.Issuer.String()] = true
}

// proofPush is one push the sending node made.
type proofPush struct {
	target *astral.Identity
	proof  *auth.SignedContract
}

// proofObjects is the sending node's objects module: it records each push and
// delivers an accepted one to the holder of the target. err fails a push, stall
// holds it until its context ends.
type proofObjects struct {
	objectsmod.Module
	mu      sync.Mutex
	pushes  []proofPush
	holders map[string]*contractHolder
	err     error
	stall   bool
}

func (o *proofObjects) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	proof, _ := obj.(*auth.SignedContract)

	err, stall, holder := o.record(target, proof)

	if stall {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(effectTimeout):
			return errors.New("the stalled push was never ended")
		}
	}
	if err != nil {
		return err
	}
	if holder != nil && proof != nil {
		holder.index(proof)
	}
	return nil
}

// record notes one push and returns how the fake answers it.
func (o *proofObjects) record(target *astral.Identity, proof *auth.SignedContract) (err error, stall bool, holder *contractHolder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pushes = append(o.pushes, proofPush{target: target, proof: proof})
	return o.err, o.stall, o.holders[target.String()]
}

func (o *proofObjects) setHolder(id *astral.Identity, h *contractHolder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holders[id.String()] = h
}

func (o *proofObjects) setStall() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stall = true
}

func (o *proofObjects) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.pushes)
}

func (o *proofObjects) setErr(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.err = err
}

func (o *proofObjects) last() proofPush {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.pushes[len(o.pushes)-1]
}

// proofFixture is a routing fixture whose node pushes through proofObjects and
// whose links lead to hosts that admit a relay for an app only after a push.
type proofFixture struct {
	*pullFixture
	objects *proofObjects
}

func newProofFixture(t *testing.T) *proofFixture {
	t.Helper()
	f := &proofFixture{pullFixture: newRoutingFixture(t), objects: &proofObjects{holders: map[string]*contractHolder{}}}
	f.mod.Objects = f.objects
	return f
}

// proofHost is a node the fixture's node has a link to.
type proofHost struct {
	id     *astral.Identity
	holder *contractHolder
	router *hostRouter
	link   *Link
}

// host builds a link from the fixture's node to a new swarm member whose router
// answers "hello". The link is added to the pool when pooled; otherwise the
// caller hands it to the module's retrieval.
func (f *proofFixture) host(t *testing.T, pooled bool) *proofHost {
	t.Helper()
	h := &proofHost{id: astral.GenerateIdentity(), holder: &contractHolder{}, router: &hostRouter{reply: "hello"}}
	f.objects.setHolder(h.id, h.holder)

	// why not net.Pipe: an unbuffered pipe deadlocks when both read loops write
	// at once, as concurrent queries make them.
	a, b := tcpPair(t)
	local := testMux(t, a, f.nodeID, h.id)

	// the host's mux: it holds the contracts a push gave it, and the fixture's
	// node is a member of its swarm.
	remote := &Mux{
		mod: &Module{
			Deps: Deps{Auth: h.holder, User: &claimUser{user: astral.GenerateIdentity(), members: []*astral.Identity{f.nodeID}}},
			node: &identityNode{id: h.id},
			log:  log.New(h.id),
		},
		ch:             channel.New(b, channel.WithLockedWrites()),
		localIdentity:  h.id,
		remoteIdentity: f.nodeID,
		routerSet:      make(chan struct{}),
	}
	remote.SetRouter(h.router)
	go func() {
		for {
			obj, err := remote.ch.Receive()
			if err != nil || remote.Handle(obj) != nil {
				return
			}
		}
	}()

	h.link = &Link{id: astral.NewNonce(), conn: &identityConn{remote: h.id}, mux: local}
	if pooled {
		if err := f.mod.linkPool.links.Add(h.link); err != nil {
			t.Fatalf("add link: %v", err)
		}
	}
	return h
}

// retrieving makes the module retrieve h's link, counting the retrievals.
func (f *proofFixture) retrieving(h *proofHost) *retrievalProbe {
	probe := &retrievalProbe{}
	f.mod.retrieve = func(ctx *astral.Context, target *astral.Identity) <-chan LinkResult {
		probe.mu.Lock()
		probe.targets = append(probe.targets, target)
		probe.mu.Unlock()
		result := make(chan LinkResult, 1)
		result <- LinkResult{Link: h.link}
		return result
	}
	return probe
}

// appProof is the relay contract app issued to the fixture's node, the proof an
// app query carries.
func (f *proofFixture) appProof(app *astral.Identity) *auth.SignedContract {
	return relayContractOf(app, f.nodeID)
}

// appQuery routes one query of app, carrying proof when it is not nil, from the
// fixture's node to target.
func (f *proofFixture) appQuery(app, target *astral.Identity, proof *auth.SignedContract) routeResult {
	ctx, cancel := f.callerCtx()
	defer cancel()
	return f.routeAs(ctx, app, target, proof)
}

func (f *proofFixture) routeAs(ctx *astral.Context, caller, target *astral.Identity, proof *auth.SignedContract) (r routeResult) {
	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      caller,
		Target:      target,
		QueryString: "app.op",
	})
	if proof != nil {
		q.Extra.Set(nodes.ExtraCallerProof, proof)
	}

	pr, pw := io.Pipe()
	start := time.Now()
	_, r.err = f.mod.RouteQuery(ctx, q, pw)
	r.took = time.Since(start)
	if r.err != nil {
		return r
	}

	answer := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		answer <- string(buf)
	}()
	select {
	case r.answer = <-answer:
	case <-time.After(effectTimeout):
		r.err = errors.New("the answer never reached the caller")
	}
	return r
}

func assertAdmitted(t *testing.T, r routeResult, h *proofHost, app *astral.Identity) {
	t.Helper()
	if r.err != nil || r.answer != "hello" {
		t.Fatalf("query answered %q, err %v; want hello", r.answer, r.err)
	}
	got := h.router.received()
	if len(got) != 1 || !got[0].caller.IsEqual(app) || !got[0].target.IsEqual(h.id) {
		t.Fatalf("host routed %v; want one query of the app for the host", got)
	}
}

func assertRefused(t *testing.T, r routeResult) {
	t.Helper()
	var rejected *astral.ErrRejected
	if !errors.As(r.err, &rejected) {
		t.Fatalf("query answered %q, err %v; want a refusal", r.answer, r.err)
	}
}

// B holds no contract of app X and a live link to B exists: the proof is pushed
// to B first and B admits the query.
// Covers G15 acceptance criterion 1.
func TestPushProofAdmitsAppQueryOverLiveLink(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	proof := f.appProof(app)

	if b.holder.holds(app) {
		t.Fatal("the host holds the app's contract before the query")
	}

	assertAdmitted(t, f.appQuery(app, b.id, proof), b, app)

	if f.objects.count() != 1 {
		t.Fatalf("%d pushes; want 1", f.objects.count())
	}
	if p := f.objects.last(); !p.target.IsEqual(b.id) || p.proof != proof {
		t.Fatalf("pushed %v to %v; want the app's proof to the host", p.proof, p.target)
	}
}

// Without a push the same host refuses the same query: the push is what admits
// it.
func TestAppQueryWithoutPushIsRefusedByAHostWithoutTheContract(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	f.objects.setErr(errors.New("push failed"))

	assertRefused(t, f.appQuery(app, b.id, f.appProof(app)))
	if got := b.router.received(); len(got) != 0 {
		t.Fatalf("host routed %v; want nothing", got)
	}
}

// No link to B exists and the module retrieves one: the proof is pushed to B
// before the query goes out over the new link, with no contract delivered by
// any other path.
// Covers G15 acceptance criterion 2.
func TestPushProofAdmitsAppQueryOverRetrievedLink(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, false)
	retrieved := f.retrieving(b)
	proof := f.appProof(app)

	assertAdmitted(t, f.appQuery(app, b.id, proof), b, app)

	if !retrieved.retrievedFor(b.id) {
		t.Fatal("no link was retrieved to the host")
	}
	if f.objects.count() != 1 || !f.objects.last().target.IsEqual(b.id) {
		t.Fatalf("%d pushes; want 1, to the host", f.objects.count())
	}
}

// A second query of the app over the same link pushes nothing; a link to
// another host pushes once; another contract over the first link pushes again.
// Covers G15 acceptance criterion 3.
func TestPushProofOncePerLinkAndContract(t *testing.T) {
	f := newProofFixture(t)
	app, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	b, c := f.host(t, true), f.host(t, true)

	f.appQuery(app, b.id, f.appProof(app))
	f.appQuery(app, b.id, f.appProof(app))
	if f.objects.count() != 1 {
		t.Fatalf("%d pushes after two queries over one link; want 1", f.objects.count())
	}
	if got := b.router.received(); len(got) != 2 {
		t.Fatalf("host routed %d queries; want 2", len(got))
	}

	f.appQuery(app, c.id, f.appProof(app))
	if f.objects.count() != 2 || !f.objects.last().target.IsEqual(c.id) {
		t.Fatalf("%d pushes after a query over another link; want 2, the last to it", f.objects.count())
	}

	f.appQuery(other, b.id, f.appProof(other))
	if f.objects.count() != 3 || !f.objects.last().proof.Issuer.IsEqual(other) {
		t.Fatalf("%d pushes after another app's query; want 3, the last for that app", f.objects.count())
	}
}

// Queries that start together over one link push the contract once.
// Covers the concurrent case of G15 acceptance criterion 3.
func TestPushProofConcurrentQueriesPushOnce(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	proof := f.appProof(app)

	const queries = 8
	results := make(chan routeResult, queries)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < queries; i++ {
		go func() {
			start.Wait()
			results <- f.appQuery(app, b.id, proof)
		}()
	}
	start.Done()

	for i := 0; i < queries; i++ {
		if r := <-results; r.err != nil || r.answer != "hello" {
			t.Fatalf("query answered %q, err %v; want hello", r.answer, r.err)
		}
	}
	if f.objects.count() != 1 {
		t.Fatalf("%d pushes for %d concurrent queries; want 1", f.objects.count(), queries)
	}
}

// A push that fails is not remembered: the query still goes out and the host
// answers by what it holds, and the next query pushes again.
// Covers G15 acceptance criterion 4.
func TestFailedProofPushIsNotCachedAndTheQueryIsSent(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	proof := f.appProof(app)
	f.objects.setErr(errors.New("push failed"))

	assertRefused(t, f.appQuery(app, b.id, proof))
	if f.objects.count() != 1 {
		t.Fatalf("%d pushes; want 1", f.objects.count())
	}

	// the host holds the contract from another path: it admits the query
	// although the push failed.
	b.holder.index(proof)
	assertAdmitted(t, f.appQuery(app, b.id, proof), b, app)
	if f.objects.count() != 2 {
		t.Fatalf("%d pushes after the failure; want 2, the failure not cached", f.objects.count())
	}

	// the push that succeeds is cached.
	f.objects.setErr(nil)
	f.appQuery(app, b.id, proof)
	f.appQuery(app, b.id, proof)
	if f.objects.count() != 3 {
		t.Fatalf("%d pushes after a success; want 3, the success cached", f.objects.count())
	}
}

// A push that never returns ends at callerProofPushTimeout and the query goes
// out after it.
func TestStalledProofPushIsBoundedAndTheQueryIsSent(t *testing.T) {
	old := callerProofPushTimeout
	callerProofPushTimeout = 150 * time.Millisecond
	t.Cleanup(func() { callerProofPushTimeout = old })

	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	proof := f.appProof(app)
	f.objects.setStall()

	r := f.appQuery(app, b.id, proof)
	assertRefused(t, r)
	if r.took < callerProofPushTimeout || r.took > effectTimeout/2 {
		t.Fatalf("query took %v; want it bounded by the %v push timeout", r.took, callerProofPushTimeout)
	}

	// the host holds the contract: the query that follows a stalled push is
	// still admitted, and pushes again.
	b.holder.index(proof)
	assertAdmitted(t, f.appQuery(app, b.id, proof), b, app)
	if f.objects.count() != 2 {
		t.Fatalf("%d pushes; want 2, the stalled push not cached", f.objects.count())
	}
}

// A query whose caller is the node itself, and an app query that carries no
// proof, push nothing.
// Covers G15 acceptance criterion 5.
func TestNoProofPushForNodeCallerOrWithoutProof(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)

	// the node is the caller; a proof in Extra is ignored.
	r := f.appQuery(f.nodeID, b.id, f.appProof(app))
	if r.err != nil || r.answer != "hello" {
		t.Fatalf("node query answered %q, err %v; want hello", r.answer, r.err)
	}
	if f.objects.count() != 0 {
		t.Fatalf("%d pushes for a query of the node; want 0", f.objects.count())
	}

	// an app query without a proof: nothing to push, and the host refuses.
	assertRefused(t, f.appQuery(app, b.id, nil))
	if f.objects.count() != 0 {
		t.Fatalf("%d pushes for a query without a proof; want 0", f.objects.count())
	}
}

// A host that holds the contract already takes the push quietly, and the query
// is admitted. A new link to such a host pushes once.
// note: the host's own handling of a held contract is mod/auth's IndexContract,
// which skips a contract already indexed (TestIndexContractSkipsAnIndexedContract, mod/auth/src/signing_test.go).
// Covers the sender half of G15 acceptance criterion 5.
func TestPushProofToAHostHoldingTheContractIsQuiet(t *testing.T) {
	f := newProofFixture(t)
	app := astral.GenerateIdentity()
	b := f.host(t, true)
	proof := f.appProof(app)
	b.holder.index(proof)

	assertAdmitted(t, f.appQuery(app, b.id, proof), b, app)
	if f.objects.count() != 1 {
		t.Fatalf("%d pushes; want 1", f.objects.count())
	}
}
