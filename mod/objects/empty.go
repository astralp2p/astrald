package objects

import (
	"bytes"
	"crypto/sha256"

	"github.com/astralp2p/astral-go/astral"
)

// emptyObjectID is the ObjectID of empty content: size 0 and the SHA-256 of no bytes.
var emptyObjectID = astral.ObjectID{Hash: sha256.Sum256(nil)}

// EmptyObjectID returns the ObjectID of the empty object.
func EmptyObjectID() *astral.ObjectID {
	var id = emptyObjectID
	return &id
}

// IsEmptyObject reports whether objectID names the empty object.
// note: the empty object is never stored; every repository answers it without storage.
// note: operator decision 2026-11-12, recorded in https://wiki.satforge.dev/doc/implement-the-vfs-module-and-the-changes-that-ship-with-it-oofFdNw03v
// why: no other content hashes to the SHA-256 of no bytes, so the one size-0 ID with that hash is never a partial ID of stored content.
func IsEmptyObject(objectID *astral.ObjectID) bool {
	return objectID != nil && *objectID == emptyObjectID
}

// OpenEmptyObject returns a reader of the empty object that names repo as its repository.
// The window follows ResolveReadLimit: offset 0 answers zero bytes, a larger offset returns ErrOutOfBounds.
func OpenEmptyObject(repo Repository, offset, limit int64) (Reader, error) {
	if _, err := ResolveReadLimit(&emptyObjectID, offset, limit); err != nil {
		return nil, err
	}

	return &emptyReader{Reader: bytes.NewReader(nil), repo: repo}, nil
}

// emptyReader reads the empty object.
// note: the embedded bytes.Reader supplies Seek, which the HTTP object handler uses.
type emptyReader struct {
	*bytes.Reader
	repo Repository
}

var _ Reader = &emptyReader{}

func (r *emptyReader) Close() error {
	return nil
}

func (r *emptyReader) Repo() Repository {
	return r.repo
}

func (r *emptyReader) ID() *astral.ObjectID {
	return EmptyObjectID()
}
