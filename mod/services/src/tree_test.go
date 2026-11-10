package services

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// memNode is an in-memory tree.Node holding one value and named sub-nodes.
type memNode struct {
	mu    sync.Mutex
	value astral.Object
	subs  map[string]*memNode
}

func newMemNode() *memNode { return &memNode{value: &astral.Nil{}, subs: map[string]*memNode{}} }

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

func (n *memNode) Delete(*astral.Context) error { return nil }

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
	sub := newMemNode()
	n.subs[name] = sub
	return sub, nil
}

type memTree struct {
	treemod.Module
	root *memNode
}

func (t *memTree) Root() tree.Node { return t.root }

// A private list already in the tree at /mod/services/private applies when
// the module binds it, before any discovery.
func TestBindVisibilityAppliesTheStoredList(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.a, "player", available)
	ctx := astral.NewContext(nil)
	root := newMemNode()
	node, err := tree.Query(ctx, root, "/mod/services/private", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Set(ctx, &services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}}); err != nil {
		t.Fatal(err)
	}
	v.a.Tree = &memTree{root: root}

	if err := v.a.bindVisibility(ctx); err != nil {
		t.Fatal(err)
	}
	if offered, _ := collect(t, open(t, v.a.OpDiscover, v.app, "services.discover", false)); len(offered) != 0 {
		t.Fatalf("offered %v; want the listed player hidden", offered)
	}
}
