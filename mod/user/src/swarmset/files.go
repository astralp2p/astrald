package swarmset

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

const (
	// Dir holds one folder per user.EntryKind, each holding entries named by their entry ID.
	Dir = "/swarmstate"
	// HeadPath holds the head in decimal. It lives outside Dir, so no sync ever reads it.
	HeadPath = "/swarmhead"
)

// Files is the part of the VFS module that keeps the entries in the VFS of the owning identity. mod/vfs
// satisfies it; tests use an in-memory one.
type Files interface {
	CreateVFS(id *astral.Identity) error
	WriteFile(ctx *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error)
	ReadFile(ctx *astral.Context, id *astral.Identity, path string) ([]byte, error)
	ListFolder(id *astral.Identity, path string) ([]string, error)
}

// EntryPath returns the path of the file of the entry of kind with entry ID id.
func EntryPath(kind user.EntryKind, id *astral.ObjectID) string {
	return Dir + "/" + kind.String() + "/" + id.String()
}
