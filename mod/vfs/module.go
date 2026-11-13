/*
Package vfs describes a module that gives each identity a tree of named folders and files.

A VFS belongs to one identity. Its entries live in the node database and its file content lives in a
repository of the objects module. See VFS 6 for the specification.
*/
package vfs

import (
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "vfs"
const DBPrefix = "vfs__"

type Module interface {
	// CreateVFS creates the VFS of an identity with the default quotas. Creating a VFS that exists succeeds and
	// changes nothing. The zero identity and the identity of the node are refused with ErrNotPermitted.
	//
	// The call is not an authorized query: it is for modules inside the node, such as apphost.
	CreateVFS(id *astral.Identity) error
}
