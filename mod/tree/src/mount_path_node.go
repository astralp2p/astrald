package tree

import (
	"context"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// mountPathNode stands for a segment on the way to a mount that its parent node does not hold.
// A write through it creates the segment in the parent node first.
type mountPathNode struct {
	parent tree.Node
	name   string
}

var _ tree.Node = &mountPathNode{}

// Get yields astral.Nil; with follow the channel stays open until ctx is done.
// why: a read never creates the segment, because tree.get runs under a read permission.
func (node *mountPathNode) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	ch := make(chan astral.Object, 1)
	ch <- &astral.Nil{}
	if !follow {
		close(ch)
		return ch, nil
	}

	// fixme: a follower is not sent the value of a later write that stores this segment.
	context.AfterFunc(ctx, func() { close(ch) })
	return ch, nil
}

func (node *mountPathNode) Set(ctx *astral.Context, object astral.Object) error {
	stored, err := node.create(ctx)
	if err != nil {
		return err
	}

	return stored.Set(ctx, object)
}

// Delete fails because a mount always lies below the segment.
func (node *mountPathNode) Delete(ctx *astral.Context) error {
	return tree.ErrNodeHasSubnodes
}

// Sub returns no subnodes; NodeWrapper adds the names that lead to mounts.
func (node *mountPathNode) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	return map[string]tree.Node{}, nil
}

func (node *mountPathNode) Create(ctx *astral.Context, name string) (tree.Node, error) {
	stored, err := node.create(ctx)
	if err != nil {
		return nil, err
	}

	return stored.Create(ctx, name)
}

// create creates the segment in the parent node, creating the parent first when it is a mountPathNode too.
func (node *mountPathNode) create(ctx *astral.Context) (tree.Node, error) {
	parent := node.parent
	if path, ok := parent.(*mountPathNode); ok {
		var err error
		if parent, err = path.create(ctx); err != nil {
			return nil, err
		}
	}

	return tree.Query(ctx, parent, node.name, true)
}
