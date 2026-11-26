package nodes

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// memFiles is a fake of the VFS calls the swarm store makes: one map of paths to content per identity.
type memFiles struct {
	mu     sync.Mutex
	vfs    map[string]map[string][]byte
	writes int
}

func newMemFiles() *memFiles { return &memFiles{vfs: map[string]map[string][]byte{}} }

func (f *memFiles) CreateVFS(id *astral.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.vfs[id.String()] == nil {
		f.vfs[id.String()] = map[string][]byte{}
	}

	return nil
}

func (f *memFiles) WriteFile(_ *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	files, ok := f.vfs[id.String()]
	if !ok {
		return nil, vfsapi.ErrNoSuchVFS
	}

	files[path] = slices.Clone(content)
	f.writes++

	return &astral.ObjectID{Size: uint64(len(content)), Hash: sha256.Sum256(content)}, nil
}

func (f *memFiles) ReadFile(_ *astral.Context, id *astral.Identity, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.vfs[id.String()][path]
	if !ok {
		return nil, vfsapi.ErrNotFound
	}

	return slices.Clone(data), nil
}

// ListFolder lists the names one level below path that some file path passes through.
func (f *memFiles) ListFolder(id *astral.Identity, path string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	seen := map[string]bool{}
	for p := range f.vfs[id.String()] {
		rest, ok := strings.CutPrefix(p, path+"/")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil, vfsapi.ErrNotFound
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

func (f *memFiles) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.writes
}

func (f *memFiles) read(id *astral.Identity, path string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.vfs[id.String()][path]

	return data, ok
}

// swarmScript is the scripted outcome of CheckSwarmEntry, shared by every node of a test swarm: an entry in drops
// is dropped, an entry in needs waits until the entry it needs is stored on the checking node, and any other
// entry is accepted.
type swarmScript struct {
	drops map[astral.ObjectID]bool
	needs map[astral.ObjectID]astral.ObjectID
}

func newSwarmScript() *swarmScript {
	return &swarmScript{drops: map[astral.ObjectID]bool{}, needs: map[astral.ObjectID]astral.ObjectID{}}
}

// scriptedChecker is a fake of user.SwarmView.CheckSwarmEntry. It reads the node's set through
// nodesmod.SwarmState, as mod/user does, and records every entry it is asked about.
type scriptedChecker struct {
	script *swarmScript
	state  nodesmod.SwarmState

	mu    sync.Mutex
	calls []astral.ObjectID
}

func (c *scriptedChecker) CheckSwarmEntry(obj astral.Object) user.EntryVerdict {
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return user.EntryDrop
	}

	c.mu.Lock()
	c.calls = append(c.calls, *id)
	c.mu.Unlock()

	if c.script.drops[*id] {
		return user.EntryDrop
	}

	if need, ok := c.script.needs[*id]; ok {
		if _, err := c.state.SwarmEntry(&need); err != nil {
			return user.EntryWait
		}
	}

	return user.EntryAccept
}

func (c *scriptedChecker) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.calls)
}

// stateNode is one node of a test swarm: its VFS, its store and its checker.
type stateNode struct {
	id      *astral.Identity
	files   *memFiles
	store   *swarmStore
	checker *scriptedChecker
	pushed  *entryPushLog // what the store passed on
}

func newStateNode(t *testing.T, script *swarmScript) *stateNode {
	t.Helper()

	n := &stateNode{id: astral.GenerateIdentity(), files: newMemFiles(), pushed: &entryPushLog{}}
	n.checker = &scriptedChecker{script: script}
	n.open(t)

	return n
}

// open opens the node's store over its VFS, as a restart does.
func (n *stateNode) open(t *testing.T) {
	t.Helper()

	ctx := astral.NewContext(nil).WithIdentity(n.id).IncludeZone(astral.ZoneNetwork)

	store, err := newSwarmStore(ctx, n.files, n.id, n.checker)
	if err != nil {
		t.Fatalf("newSwarmStore: %v", err)
	}

	if n.store != nil {
		store.peers = n.store.peers
	}
	store.passOnWait = true
	n.store, n.checker.state = store, store
}

// entryPushLog records the entries a store passes on.
type entryPushLog struct {
	mu  sync.Mutex
	ids []astral.ObjectID
}

func (l *entryPushLog) add(obj astral.Object) {
	id, _ := astral.ResolveObjectID(obj)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.ids = append(l.ids, *id)
}

func (l *entryPushLog) has(id astral.ObjectID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Contains(l.ids, id)
}

func (l *entryPushLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.ids)
}

// recordingPusher logs each entry before it reaches the peer.
type recordingPusher struct {
	log  *entryPushLog
	next swarmPusher
}

func (p recordingPusher) SwarmPush(ctx *astral.Context, obj astral.Object) error {
	p.log.add(obj)
	return p.next.SwarmPush(ctx, obj)
}

// connect makes the store of n pass its entries on to each peer, through n's push log.
func (n *stateNode) connect(peers ...swarmPusher) {
	var list []swarmPusher
	for _, p := range peers {
		list = append(list, recordingPusher{log: n.pushed, next: p})
	}

	n.store.peers = func() []swarmPusher { return list }
}

// testSig is a signature placeholder: the scripted checker never verifies one.
func testSig() *crypto.Signature {
	return &crypto.Signature{Scheme: "test", Data: astral.Bytes16{1, 2, 3}}
}

func entryID(t *testing.T, obj astral.Object) astral.ObjectID {
	t.Helper()

	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	return *id
}

func membershipEntry(t *testing.T, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()

	c, err := user.NewMembership(issuer, subject, user.TierTrusted, nil, time.Hour)
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}

	return &auth.SignedContract{Contract: c, IssuerSig: testSig()}
}

func joinRequestEntry(joiner, carrier *astral.Identity) *user.SignedJoinRequest {
	return &user.SignedJoinRequest{JoinRequest: user.NewJoinRequest(joiner, carrier, user.TierMember), JoinerSig: testSig()}
}

func approvalEntry(t *testing.T, issuer *astral.Identity, request astral.Object) *user.SignedApproval {
	t.Helper()

	id := entryID(t, request)

	return &user.SignedApproval{Approval: user.NewApproval(issuer, &id), IssuerSig: testSig()}
}

func declineEntry(t *testing.T, issuer *astral.Identity, request astral.Object) *user.SignedDecline {
	t.Helper()

	id := entryID(t, request)

	return &user.SignedDecline{Decline: user.NewDecline(issuer, &id), IssuerSig: testSig()}
}

func expelEntry(issuer, subject *astral.Identity) *user.SignedExpulsion {
	return &user.SignedExpulsion{
		Expulsion: &user.Expulsion{Issuer: issuer, Subject: subject, ExpelledAt: astral.Now()},
		IssuerSig: testSig(),
	}
}

// fiveEntries returns one entry of each kind.
func fiveEntries(t *testing.T) []astral.Object {
	t.Helper()

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	req := joinRequestEntry(a, b)

	return []astral.Object{req, approvalEntry(t, b, req), declineEntry(t, b, req), membershipEntry(t, b, a), expelEntry(b, a)}
}

// storedIDs returns the stored entry IDs of every kind, sorted.
func storedIDs(t *testing.T, s *swarmStore) []string {
	t.Helper()

	var ids []string
	for _, kind := range user.EntryKinds {
		list, err := s.SwarmEntries(kind)
		if err != nil {
			t.Fatalf("SwarmEntries(%v): %v", kind, err)
		}
		for _, id := range list {
			ids = append(ids, id.String())
		}
	}
	slices.Sort(ids)

	return ids
}

func mustAdd(t *testing.T, n *stateNode, obj astral.Object) {
	t.Helper()

	if err := n.store.AddSwarmEntry(n.store.ctx, obj); err != nil {
		t.Fatalf("AddSwarmEntry(%v): %v", obj.ObjectType(), err)
	}
}

// TestSwarmStoreLayoutAndRead: each kind is stored at swarmstate/<kind>/<entry ID> in the node's own VFS, reads
// back as its signed type, and is listed under its kind only; the head file is outside swarmstate/.
func TestSwarmStoreLayoutAndRead(t *testing.T) {
	n := newStateNode(t, newSwarmScript())

	for _, obj := range fiveEntries(t) {
		mustAdd(t, n, obj)

		id := entryID(t, obj)
		kind, _ := user.EntryKindOf(obj)
		if _, ok := n.files.read(n.id, "/swarmstate/"+kind.String()+"/"+id.String()); !ok {
			t.Fatalf("%v: no file at swarmstate/%v/%v", obj.ObjectType(), kind, &id)
		}

		got, err := n.store.SwarmEntry(&id)
		if err != nil {
			t.Fatalf("SwarmEntry(%v): %v", obj.ObjectType(), err)
		}
		if got.ObjectType() != obj.ObjectType() || entryID(t, got) != id {
			t.Fatalf("SwarmEntry = %v %v, want %v %v", got.ObjectType(), entryID(t, got), obj.ObjectType(), id)
		}

		ids, _ := n.store.SwarmEntries(kind)
		if len(ids) != 1 || *ids[0] != id {
			t.Fatalf("SwarmEntries(%v) = %v, want [%v]", kind, ids, id)
		}
	}

	if head, ok := n.files.read(n.id, "/swarmhead"); !ok || string(head) != "5" {
		t.Fatalf("head file = %q, %v; want 5", head, ok)
	}
	if _, err := n.store.SwarmEntries("bogus"); err == nil {
		t.Fatal("SwarmEntries accepted an invalid kind")
	}
	if err := n.store.AddSwarmEntry(n.store.ctx, astral.NewString8("x")); err == nil {
		t.Fatal("AddSwarmEntry accepted a non-entry")
	}
	if err := n.store.AddSwarmEntry(n.store.ctx, &user.SignedApproval{}); err == nil {
		t.Fatal("AddSwarmEntry accepted an empty entry")
	}
}

// TestSwarmDuplicateIsNoOp (Done: a duplicate is a no-op): an entry offered again, locally or by a peer that
// passes it straight back, returns nil and is not checked, written, counted or passed on again.
func TestSwarmDuplicateIsNoOp(t *testing.T) {
	script := newSwarmScript()
	a, b := newStateNode(t, script), newStateNode(t, script)
	a.connect(localPeer{b})
	b.connect(localPeer{a})

	obj := fiveEntries(t)[0]
	mustAdd(t, a, obj)

	checksA, writesA, pushesA, head := a.checker.callCount(), a.files.writeCount(), a.pushed.count(), a.store.SwarmHead()
	if b.store.SwarmHead() != 1 || head != 1 {
		t.Fatalf("heads = %v, %v; want 1, 1", head, b.store.SwarmHead())
	}

	for range 2 {
		if err := a.store.AddSwarmEntry(a.store.ctx, obj); err != nil {
			t.Fatalf("duplicate: %v", err)
		}
	}
	if err := b.store.AddSwarmEntry(b.store.ctx, obj); err != nil {
		t.Fatalf("duplicate on b: %v", err)
	}

	switch {
	case a.checker.callCount() != checksA:
		t.Fatalf("a duplicate was checked: %v checks, want %v", a.checker.callCount(), checksA)
	case a.files.writeCount() != writesA:
		t.Fatalf("a duplicate was written: %v writes, want %v", a.files.writeCount(), writesA)
	case a.pushed.count() != pushesA:
		t.Fatalf("a duplicate was passed on: %v pushes, want %v", a.pushed.count(), pushesA)
	case a.store.SwarmHead() != 1 || b.store.SwarmHead() != 1:
		t.Fatalf("heads = %v, %v after duplicates; want 1, 1", a.store.SwarmHead(), b.store.SwarmHead())
	}
}

// TestSwarmHeadChangesExactlyWhenEntriesArrive (Done: head changes exactly when entries arrive): the head grows
// by one per stored entry, including held entries when they are admitted, and stays put on a duplicate, a drop, a
// wait, a pull with nothing new and a reopen. It counts this node's copy and is never offered to a peer.
func TestSwarmHeadChangesExactlyWhenEntriesArrive(t *testing.T) {
	script := newSwarmScript()
	n := newStateNode(t, script)
	es := fiveEntries(t)
	req, approval, decline, member, expel := es[0], es[1], es[2], es[3], es[4]

	script.drops[entryID(t, expel)] = true
	script.needs[entryID(t, approval)] = entryID(t, req)
	script.needs[entryID(t, decline)] = entryID(t, req)

	steps := []struct {
		name string
		obj  astral.Object
		want uint64
	}{
		{"accept", member, 1},
		{"duplicate", member, 1},
		{"drop", expel, 1},
		{"wait", approval, 1},
		{"wait again", decline, 1},
		{"admits the request and both held answers", req, 4},
	}
	for _, step := range steps {
		_ = n.store.AddSwarmEntry(n.store.ctx, step.obj)
		if got := n.store.SwarmHead(); got != step.want {
			t.Fatalf("%v: head = %v, want %v", step.name, got, step.want)
		}
	}

	src := newStateNode(t, script)
	mustAdd(t, src, member)
	if added, err := n.store.pullSwarm(n.store.ctx, localPeer{src}); err != nil || added != 0 || n.store.SwarmHead() != 4 {
		t.Fatalf("pull with nothing new: added %v, %v, head %v; want 0, nil, 4", added, err, n.store.SwarmHead())
	}

	n.open(t)
	if got := n.store.SwarmHead(); got != 4 {
		t.Fatalf("head after reopen = %v, want 4", got)
	}
	if len(n.store.swarmDigest()) != 4 {
		t.Fatalf("digest has %v IDs, want the 4 entries only", len(n.store.swarmDigest()))
	}
}

// TestSwarmReopenRecountsHead: a head file that lags the entries is rewritten at open.
func TestSwarmReopenRecountsHead(t *testing.T) {
	n := newStateNode(t, newSwarmScript())
	for _, obj := range fiveEntries(t)[:3] {
		mustAdd(t, n, obj)
	}

	if _, err := n.files.WriteFile(nil, n.id, "/swarmhead", []byte("1")); err != nil {
		t.Fatal(err)
	}

	n.open(t)
	if head, _ := n.files.read(n.id, "/swarmhead"); n.store.SwarmHead() != 3 || string(head) != "3" {
		t.Fatalf("head = %v, file %q; want 3", n.store.SwarmHead(), head)
	}
}

// TestSwarmHeldSetIsBounded: past swarmHeldLimit, the oldest held entry is dropped first.
func TestSwarmHeldSetIsBounded(t *testing.T) {
	script := newSwarmScript()
	n := newStateNode(t, script)
	never := astral.ObjectID{Size: 1}

	var first astral.ObjectID
	for i := range swarmHeldLimit + 1 {
		obj := joinRequestEntry(astral.GenerateIdentity(), n.id)
		id := entryID(t, obj)
		script.needs[id] = never
		if i == 0 {
			first = id
		}
		if err := n.store.AddSwarmEntry(n.store.ctx, obj); !errors.Is(err, nodesmod.ErrSwarmEntryHeld) {
			t.Fatalf("entry %v: err = %v, want ErrSwarmEntryHeld", i, err)
		}
	}

	if len(n.store.merge.held) != swarmHeldLimit {
		t.Fatalf("held = %v, want %v", len(n.store.merge.held), swarmHeldLimit)
	}
	if n.store.merge.held[0].id == first {
		t.Fatal("the oldest held entry was kept")
	}
}

// blackHolePeer is a peer whose push never answers: it returns only when the
// push's context ends, and reports that end on ended.
type blackHolePeer struct{ ended chan error }

func (p blackHolePeer) SwarmPush(ctx *astral.Context, _ astral.Object) error {
	<-ctx.Done()
	p.ended <- ctx.Err()
	return ctx.Err()
}

// TestSwarmPassOnDoesNotWaitForADeadPeer: an entry accepted while a peer's
// push never answers is stored and AddSwarmEntry returns at once; the push
// ends at its bound.
//
// why: passOn waited for every push, so a link that takes bytes and never
// answers held up the writer (an answer op, a host signing an app's contract)
// until the link closed (reconnect-sync-after-dead-link).
func TestSwarmPassOnDoesNotWaitForADeadPeer(t *testing.T) {
	a := newStateNode(t, newSwarmScript())
	a.store.passOnWait = false
	a.store.pushTimeout = 300 * time.Millisecond
	dead := blackHolePeer{ended: make(chan error, 1)}
	a.store.peers = func() []swarmPusher { return []swarmPusher{dead} }

	start := time.Now()
	mustAdd(t, a, fiveEntries(t)[0])
	if took := time.Since(start); took > 150*time.Millisecond {
		t.Fatalf("AddSwarmEntry took %v with a dead peer; want it not to wait for the push", took)
	}
	if a.store.SwarmHead() != 1 {
		t.Fatalf("head = %v; want 1", a.store.SwarmHead())
	}

	select {
	case err := <-dead.ended:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the push ended with %v; want its deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the push to the dead peer never ended")
	}
}
