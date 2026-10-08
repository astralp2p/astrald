package tree

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// racedNode holds subnodes, and its first Sub hides one of them, as when a concurrent caller
// creates that subnode between this caller's Sub and Create.
type racedNode struct {
	NilNode
	subs   map[string]tree.Node
	hidden string
}

func (n *racedNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	subs := map[string]tree.Node{}
	for name, sub := range n.subs {
		if name != n.hidden {
			subs[name] = sub
		}
	}
	n.hidden = ""
	return subs, nil
}

func (n *racedNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	if _, ok := n.subs[name]; ok {
		return nil, tree.ErrAlreadyExists
	}
	sub := &racedNode{subs: map[string]tree.Node{}}
	n.subs[name] = sub
	return sub, nil
}

// Query descends into a segment a concurrent caller created first, and creates the rest under it.
func TestQueryDescendsIntoConcurrentlyCreatedNode(t *testing.T) {
	ctx := astral.NewContext(context.Background())
	mod := &racedNode{subs: map[string]tree.Node{}}
	root := &racedNode{subs: map[string]tree.Node{"mod": mod}, hidden: "mod"}

	got, err := Query(ctx, root, "/mod/tcp", true)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if _, ok := root.subs["tcp"]; ok {
		t.Error("Query created /tcp, want /mod/tcp")
	}
	if want := mod.subs["tcp"]; want == nil || got != want {
		t.Errorf("Query returned %v, want /mod/tcp %v", got, want)
	}
}

// Query without create answers an error for a missing segment and creates nothing.
func TestQueryMissingWithoutCreate(t *testing.T) {
	ctx := astral.NewContext(context.Background())
	root := &racedNode{subs: map[string]tree.Node{}}

	if _, err := Query(ctx, root, "/mod/tcp", false); err == nil {
		t.Error("Query of a missing path without create succeeded")
	}
	if len(root.subs) != 0 {
		t.Errorf("Query without create created %v", root.subs)
	}
}
