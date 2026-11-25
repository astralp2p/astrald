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

	// WriteFile creates or replaces the file at path in the VFS of an identity with content, creating the missing
	// folders on the way, and returns the ObjectID of the content. The path is absolute, as in vfs.write. Content of
	// zero bytes is refused and changes nothing. A VFS that does not exist is refused with vfsapi.ErrNoSuchVFS.
	//
	// The call is not an authorized query: it is for modules inside the node, such as nodes.
	WriteFile(ctx *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error)

	// ReadFile returns the content of the file at path in the VFS of an identity, read from the local group as
	// vfs.read does. A path with no entry answers vfsapi.ErrNotFound, and a folder vfsapi.ErrIsAFolder.
	//
	// The call is not an authorized query: it is for modules inside the node, such as nodes.
	ReadFile(ctx *astral.Context, id *astral.Identity, path string) ([]byte, error)

	// ListFolder returns the names of the entries of the folder at path in the VFS of an identity, in ascending
	// byte order. A path with no entry answers vfsapi.ErrNotFound, and a file vfsapi.ErrNotAFolder.
	//
	// The call is not an authorized query: it is for modules inside the node, such as nodes.
	ListFolder(id *astral.Identity, path string) ([]string, error)
}
