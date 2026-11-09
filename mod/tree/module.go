/*
Package tree describes a module that adds a tree object store to the node.

Every node in the tree can hold an Object and can have named subnodes (both at the same time are allowed).
By default, all tree nodes are stored in the database.

Paths begin with a slash and consist of segments separated by slashes, just like in a typical filesystem:

* /               - root node
* /path/to/a/node - a deeper node

Segments can contain any non-slash printable characters.

The default node implementation is a simple database store. A module compiled into the node mounts another
implementation at a path, which is how a module serves a computed value from the tree. Mounting is
in-process: no operation exposes it over the wire.

A mount is reachable whether or not its path is stored. A traversal lists each segment on the way to a mount
next to the stored subnodes. A segment that only a mount needs reads as nil and refuses deletion, and a write
through that segment stores it. Unmounting restores the stored node, if any, and drops the segments that only
the mount needed.
*/
package tree

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "tree"
const DBPrefix = "tree__"

type Module interface {
	// Root returns the root node of the tree
	Root() tree.Node

	// Set sets the object held by the node
	Set(ctx *astral.Context, path string, object astral.Object) error

	// Get returns the object held by the node
	Get(ctx *astral.Context, path string) (astral.Object, error)

	// Delete deletes the node
	Delete(ctx *astral.Context, path string) error

	// Mount mounts a node at the given path. This node will be returned whenever a traversal reaches this path.
	// The path need not be stored, and Mount stores nothing. The root "/" is immutable and cannot be mounted over.
	Mount(path string, node tree.Node) error

	// Unmount unmounts a node mounted at the given path. A node stored at the path becomes reachable again.
	// The root "/" cannot be unmounted.
	Unmount(path string) error
}
