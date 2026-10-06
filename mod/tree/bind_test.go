package tree

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// memNode is an in-memory tree.Node that holds only subnodes.
type memNode struct {
	NilNode
	subs map[string]tree.Node
}

func (n *memNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	return n.subs, nil
}

func (n *memNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	sub := &memNode{subs: map[string]tree.Node{}}
	n.subs[name] = sub
	return sub, nil
}

type bindSkipped struct {
	Value tree.Value[*astral.Uint8]
}

type bindTarget struct {
	Skipped *bindSkipped `tree:";skip"`
	Num     *int
	Level   *tree.Value[*astral.Uint8]
}

// Bind leaves nil skipped and non-struct pointer fields nil and still allocates bound ones.
func TestBindLeavesUnboundPointersNil(t *testing.T) {
	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	defer cancel()
	root := &memNode{subs: map[string]tree.Node{}}

	var s bindTarget
	if err := Bind(ctx, &s, root); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if s.Skipped != nil {
		t.Errorf("skip-tagged field Skipped = %v, want nil", s.Skipped)
	}
	if s.Num != nil {
		t.Errorf("non-struct field Num = %v, want nil", *s.Num)
	}
	if s.Level == nil {
		t.Fatal("bound field Level is nil")
	}
	if _, ok := root.subs["level"]; !ok {
		t.Error("Bind did not bind Level to /level")
	}
	if _, ok := root.subs["skipped"]; ok {
		t.Error("Bind bound the skip-tagged field")
	}
}
