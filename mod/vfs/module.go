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
	// changes nothing. Any non-zero identity is accepted, the node's included. The zero identity is refused with
	// ErrNotPermitted.
	//
	// The call is not an authorized query: it is for modules inside the node, such as apphost.
	CreateVFS(id *astral.Identity) error

	// SetEvaluator names the evaluator of the VFS of an identity: the endpoint,
	// astral://<identity-or-alias>:vfs.authorize, that the node asks about a reader or a writer its own rules refuse.
	// The endpoint takes no caller part and no arguments, and is at most 255 bytes. An empty endpoint clears it. A VFS
	// that does not exist is refused with vfsapi.ErrNoSuchVFS, and then any other endpoint with
	// vfsapi.ErrInvalidEndpoint.
	//
	// The call is not an authorized query: it is for modules inside the node, such as apphost.
	SetEvaluator(id *astral.Identity, endpoint string) error
}
