package fs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"os"
	"path/filepath"
	"sync/atomic"
)

var _ objects.Writer = &Writer{}

const tempFilePrefix = ".tmp."

// Writer streams object data to a randomly named temp file and computes the content-addressed
// ObjectID on the fly; Commit renames the temp file to its final name, Discard removes it.
// Either Commit or Discard must be called exactly once.
type Writer struct {
	repo      *Repository
	path      string
	tempID    string
	file      *os.File
	resolver  *astral.WriteResolver
	finalized atomic.Bool
}

func NewWriter(repo *Repository, path string) (*Writer, error) {
	var rbytes = make([]byte, 8)
	rand.Read(rbytes)

	var tempID = tempFilePrefix + hex.EncodeToString(rbytes)

	file, err := os.Create(filepath.Join(path, tempID))
	if err != nil {
		return nil, err
	}

	resolver := astral.NewWriteResolver(nil)

	return &Writer{
		repo:     repo,
		path:     path,
		tempID:   tempID,
		file:     file,
		resolver: resolver,
	}, nil
}

// Write feeds bytes to both the backing file and the content-address resolver simultaneously.
func (w *Writer) Write(p []byte) (n int, err error) {
	n, err = w.file.Write(p)

	if n > 0 {
		w.resolver.Write(p[:n])
	}

	return n, err
}

// Commit finalizes the object: renames the temp file to its content-addressed name,
// deduplicates silently if a file with that name already exists, then notifies the repository.
// Returns an error if the writer was already closed or the rename fails.
func (w *Writer) Commit() (*astral.ObjectID, error) {
	if !w.finalized.CompareAndSwap(false, true) {
		return nil, errors.New("writer closed")
	}

	w.file.Close()

	objectID := w.resolver.Resolve()

	stored, err := w.publish(objectID)
	if !stored {
		return nil, err
	}

	// why: pushAdded takes mu, which publish holds, so it runs after publish returns.
	w.repo.pushAdded(objectID)

	return objectID, nil
}

// publish moves the temp file to the object's path and adds the object to the index.
// publish reports false when the object path is not a regular file afterwards.
// why: mu orders the move and its index entry with Delete and index refresh.
func (w *Writer) publish(objectID *astral.ObjectID) (bool, error) {
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()

	var oldPath = filepath.Join(w.path, w.tempID)
	var newPath = filepath.Join(w.path, objectID.String())

	stat, err := os.Stat(newPath)
	if err == nil && stat.Mode().IsRegular() {
		// we already have this object
		os.Remove(oldPath)
	} else {
		err = os.Rename(oldPath, newPath)
		if err != nil {
			os.Remove(oldPath)
		}
	}

	// make sure the path is accessible
	stat, err = os.Stat(newPath)
	if err != nil || !stat.Mode().IsRegular() {
		return false, err
	}

	w.repo.addObjectLocked(objectID)

	return true, nil
}

func (w *Writer) Discard() error {
	if !w.finalized.CompareAndSwap(false, true) {
		return errors.New("writer closed")
	}

	w.file.Close()
	os.Remove(filepath.Join(w.path, w.tempID))
	return nil
}
