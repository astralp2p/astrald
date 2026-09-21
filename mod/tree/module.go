/*
Package tree describes a module that adds a tree object store to the node.

Every node in the tree can hold an Object and can have named subnodes (both at the same time are allowed).
By default, all tree nodes are stored in the database.

Paths begin with a slash and consist of segments separated by slashes, just like in a typical filesystem:

* /               - root node
* /path/to/a/node - a deeper node

Segments can contain any non-slash printable characters.

The default node implementation is a simple database store. A module compiled into the node mounts another
implementation over an existing path, which is how a module serves a computed value from the tree. Mounting is
in-process: no operation exposes it over the wire.

A mount is reachable only where the path already exists in the database, every segment included, because a
traversal overlays a mount onto a name the underlying node returns. A module therefore creates the path before
it mounts over it. Unmounting restores the stored node and leaves the path in place.
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
	// The path must already exist in the tree; Mount does not create it.
	Mount(path string, node tree.Node) error

	// Unmount unmounts a node mounted at the given path. The path itself stays in the tree.
	Unmount(path string) error
}
