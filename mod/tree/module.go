/*
Package tree describes a module that adds a tree object store to the node.

Every node in the tree can hold an Object and can have named subnodes (both at the same time are allowed).
By default, all tree nodes are stored in the database. You can mount any Node at any valid path.

Paths begin with a slash and consist of segments separated by slashes, just like in a typical filesystem:

* /               - root node
* /path/to/a/node - a deeper node

Segments can contain any non-slash printable characters.

The default node implementation is a simple database store, but you can mount any implementation at any existing
path in the tree.
*/
package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

const ModuleName = "tree"
const DBPrefix = "tree__"

const (
	MethodGet         = "tree.get"
	MethodSet         = "tree.set"
	MethodDelete      = "tree.delete"
	MethodList        = "tree.list"
	MethodMountRemote = "tree.mount_remote"
	MethodUnmount     = "tree.unmount"
)

type Module interface {
	// Root returns the root node of the tree
	Root() Node

	// Set sets the object held by the node
	Set(ctx *astral.Context, path string, object astral.Object) error

	// Get returns the object held by the node
	Get(ctx *astral.Context, path string) (astral.Object, error)

	// Delete deletes the node
	Delete(ctx *astral.Context, path string) error

	// Mount mounts a node at the given path. This node will be returned whenever a traversal reaches this path.
	Mount(path string, node Node) error

	// Unmount unmounts a node mounted at the given path.
	Unmount(path string) error

	// MountRemote mounts a remote node at the given path.
	MountRemote(ctx *astral.Context, path string, targetID *astral.Identity, remotePath string) error
}
