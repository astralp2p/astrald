package indexing

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// memNode is an in-memory tree.Node holding one value and named sub-nodes, which
// is all a registration and its cursors use.
type memNode struct {
	mu     sync.Mutex
	parent *memNode
	name   string
	value  astral.Object
	subs   map[string]*memNode
}

var _ tree.Node = &memNode{}

func newMemNode(parent *memNode, name string) *memNode {
	return &memNode{parent: parent, name: name, value: &astral.Nil{}, subs: map[string]*memNode{}}
}

// note: follow is ignored; the channel carries the current value and closes.
func (n *memNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan astral.Object, 1)
	ch <- n.value
	close(ch)
	return ch, nil
}

func (n *memNode) Set(_ *astral.Context, object astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.value = object
	return nil
}

func (n *memNode) Delete(*astral.Context) error {
	n.parent.mu.Lock()
	defer n.parent.mu.Unlock()

	delete(n.parent.subs, n.name)
	return nil
}

func (n *memNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	subs := make(map[string]tree.Node, len(n.subs))
	for name, sub := range n.subs {
		subs[name] = sub
	}
	return subs, nil
}

func (n *memNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, ok := n.subs[name]; ok {
		return nil, tree.ErrAlreadyExists
	}
	sub := newMemNode(n, name)
	n.subs[name] = sub
	return sub, nil
}

// closeWriter is the caller's end of the connection.
//
// why: the op answers and then closes its channel in a defer, so Close marks
// that the unregistration has run.
type closeWriter struct {
	closed atomic.Bool
	done   chan struct{}
}

func newCloseWriter() *closeWriter {
	return &closeWriter{done: make(chan struct{})}
}

func (w *closeWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *closeWriter) Close() error {
	if w.closed.CompareAndSwap(false, true) {
		close(w.done)
	}
	return nil
}

// mountLikeShell mounts the module's ops as its loader does and scopes the
// module's router by module name as mod/shell does for every loaded module.
func mountLikeShell(t *testing.T, mod *Module) *routing.ScopeRouter {
	t.Helper()

	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		t.Fatalf("mount ops: %v", err)
	}

	scopes := routing.NewScopeRouter(nil)
	scopes.Add(astral.Stringify(mod), mod.Router())
	return scopes
}

// unregister sends indexing.unregister_indexer through scopes and returns once
// the op has closed its channel.
func unregister(t *testing.T, ctx *astral.Context, scopes *routing.ScopeRouter, nonce astral.Nonce) {
	t.Helper()

	caller := astral.GenerateIdentity()
	q := query.New(caller, caller, "indexing.unregister_indexer?nonce="+nonce.String(), nil)
	w := newCloseWriter()

	if _, err := scopes.RouteQuery(ctx, astral.Launch(q), w); err != nil {
		t.Fatalf("route indexing.unregister_indexer: %v", err)
	}

	select {
	case <-w.done:
	case <-ctx.Done():
		t.Fatal("indexing.unregister_indexer did not close its channel")
	}
}

// TestUnregisterIndexerEndsTheRegistration routes indexing.unregister_indexer by
// its full name and checks the lifecycle it ends.
//
// The module holds only the indexers tree — no database, no objects, no auth.
// An op that reached stored objects or the repository change log would panic on
// a nil field, so leaving them untouched is enforced by construction.
func TestUnregisterIndexerEndsTheRegistration(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	root := newMemNode(nil, "")
	mod := &Module{indexers: root}
	scopes := mountLikeShell(t, mod)

	if !scopes.HasRoute("indexing.unregister_indexer") {
		t.Fatal("the node does not route indexing.unregister_indexer")
	}
	if scopes.HasRoute("indexing.remove_index") {
		t.Fatal("the node still routes indexing.remove_index")
	}

	nonce, err := mod.RegisterIndexer(ctx, "search")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mod.UpdateIndexerState(ctx, nonce, "local", 1); err != nil {
		t.Fatalf("advance cursor: %v", err)
	}
	registrations, _ := root.Sub(ctx)
	registration := registrations["search"]

	unregister(t, ctx, scopes, nonce)

	if subs, _ := root.Sub(ctx); len(subs) != 0 {
		t.Fatalf("%d registrations remain; want none", len(subs))
	}
	if subs, _ := registration.Sub(ctx); len(subs) != 0 {
		t.Fatalf("%d cursors remain; want none", len(subs))
	}
	if err := mod.UpdateIndexerState(ctx, nonce, "local", 2); !errors.Is(err, indexing.ErrIndexNotFound) {
		t.Fatalf("advance on the unregistered nonce: got %v, want %v", err, indexing.ErrIndexNotFound)
	}

	fresh, err := mod.RegisterIndexer(ctx, "search")
	if err != nil {
		t.Fatalf("register again: %v", err)
	}
	if fresh == nonce {
		t.Fatalf("registration after unregister kept nonce %v; want a new one", nonce)
	}

	idxer, err := mod.findIndexerByNonce(ctx, fresh)
	if err != nil || idxer == nil {
		t.Fatalf("find the new registration: %v, %v", idxer, err)
	}
	if version, err := idxer.state(ctx, "local"); err != nil || version != 0 {
		t.Fatalf("new registration starts at version %d (err %v); want 0", version, err)
	}
}
