package tree

import (
	"strings"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// NodeWrapper wraps a tree.Node and overlays the nodes mounted below its path onto its subnodes.
type NodeWrapper struct {
	tree.Node
	path []string
	mod  *Module
}

var _ tree.Node = &NodeWrapper{}

// Sub returns the subnodes of the wrapped node with mounts overlaid. A name on the way to a mount is
// listed even when the wrapped node does not hold it.
func (wrap *NodeWrapper) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	stored, err := wrap.Node.Sub(ctx)
	if err != nil {
		return nil, err
	}

	sub := make(map[string]tree.Node, len(stored))
	for name, node := range stored {
		sub[name] = wrap.wrapSub(name, node)
	}

	for _, name := range wrap.mod.findMountNames(wrap.Path()) {
		if _, found := sub[name]; !found {
			sub[name] = wrap.wrapSub(name, &mountPathNode{parent: wrap.Node, name: name})
		}
	}

	return sub, nil
}

// Create creates a subnode in the wrapped node. A name already served by a mount answers tree.ErrAlreadyExists.
func (wrap *NodeWrapper) Create(ctx *astral.Context, name string) (tree.Node, error) {
	if wrap.mod.getMount(wrap.subPath(name)) != nil {
		return nil, tree.ErrAlreadyExists
	}

	node, err := wrap.Node.Create(ctx, name)
	if err != nil {
		return nil, err
	}

	return wrap.wrapSub(name, node), nil
}

// wrapSub wraps the subnode name, replaced by the node mounted at its path if there is one.
func (wrap *NodeWrapper) wrapSub(name string, node tree.Node) *NodeWrapper {
	if mounted := wrap.mod.getMount(wrap.subPath(name)); mounted != nil {
		node = mounted
	}

	return &NodeWrapper{
		path: append(wrap.path[:len(wrap.path):len(wrap.path)], name),
		Node: node,
		mod:  wrap.mod,
	}
}

func (wrap *NodeWrapper) Path() string {
	return "/" + strings.Join(wrap.path, "/")
}

// subPath returns the absolute path of the subnode name.
func (wrap *NodeWrapper) subPath(name string) string {
	return strings.TrimSuffix(wrap.Path(), "/") + "/" + name
}
