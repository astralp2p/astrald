package tree

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
)

// NodeWrapper wraps a tree.Node and replaces returned Node values with nodes mounted at those paths.
type NodeWrapper struct {
	tree.Node
	path []string
	mod  *Module
}

var _ tree.Node = &NodeWrapper{}

func (wrap *NodeWrapper) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	sub, err := wrap.Node.Sub(ctx)
	if err != nil {
		return nil, err
	}

	path := strings.TrimSuffix(wrap.Path(), "/")

	// wrap sub nodes
	for name, node := range sub {
		subPath := path + "/" + name

		// check if there's something mounted at this path
		if mounted := wrap.mod.getMount(subPath); mounted != nil {
			node = mounted
		}

		// add the node wrapper
		sub[name] = &NodeWrapper{
			path: append(wrap.path, name),
			Node: node,
			mod:  wrap.mod,
		}
	}

	return sub, err
}

func (wrap *NodeWrapper) Create(ctx *astral.Context, name string) (tree.Node, error) {
	node, err := wrap.Node.Create(ctx, name)
	if err != nil {
		return nil, err
	}

	return &NodeWrapper{
		path: append(wrap.path, name),
		Node: node,
		mod:  wrap.mod,
	}, nil
}

func (wrap *NodeWrapper) Path() string {
	return "/" + strings.Join(wrap.path, "/")
}
