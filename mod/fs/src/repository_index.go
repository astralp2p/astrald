package fs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// entryObjectID returns the full ID a directory entry is named by, or nil when the entry is not an object file.
// why: only the canonical data1 form names a stored object, so a data0 name or a data1 name with extra leading 'y' is skipped.
func entryObjectID(entry os.DirEntry) *astral.ObjectID {
	if !entry.Type().IsRegular() {
		return nil
	}

	// why: ParseID panics on an over-long data1 body.
	// note: a canonical name has at most 64 characters after its prefix, so the check skips no object file.
	if len(entry.Name()) > len("data1")+64 {
		return nil
	}

	objectID, err := astral.ParseID(entry.Name())
	if err != nil || objectID.String() != entry.Name() {
		return nil
	}

	return objectID
}

// findObject returns the full ID of the stored object objectID names.
// A full ID is returned as a copy, unchecked, because it names its file directly.
// A partial ID resolves through the hash index under mu.
func (repo *Repository) findObject(ctx *astral.Context, objectID *astral.ObjectID) (*astral.ObjectID, error) {
	if objectID.Size != 0 {
		var id = *objectID
		return &id, nil
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	return repo.findPartialLocked(ctx, objectID.Hash)
}

// findPartialLocked returns the one stored full ID with the hash. The caller holds mu.
// why: a lookup refreshes the index at most once, and a cold lookup's first listing is that refresh.
func (repo *Repository) findPartialLocked(ctx *astral.Context, hash [32]byte) (*astral.ObjectID, error) {
	var cold = repo.ids == nil
	if cold {
		if err := repo.refreshIndexLocked(ctx); err != nil {
			return nil, err
		}
	}

	objectID, err := repo.matchLocked(hash)
	if cold || !errors.Is(err, objectsmod.ErrNotFound) {
		return objectID, err
	}

	if err = repo.refreshIndexLocked(ctx); err != nil {
		return nil, err
	}

	return repo.matchLocked(hash)
}

// matchLocked returns the one indexed full ID with the hash whose file is a regular file of its size.
// matchLocked evicts every other candidate. The caller holds mu.
func (repo *Repository) matchLocked(hash [32]byte) (*astral.ObjectID, error) {
	candidates, _ := repo.ids.Get(hash)

	var found []astral.ObjectID
	for _, candidate := range candidates {
		ok, err := repo.isObjectFile(&candidate)
		if err != nil {
			return nil, err
		}
		if ok {
			found = append(found, candidate)
		}
	}

	repo.setCandidatesLocked(hash, found)

	switch len(found) {
	case 0:
		return nil, objectsmod.ErrNotFound
	case 1:
		var objectID = found[0]
		return &objectID, nil
	}

	return nil, objectsmod.ErrAmbiguousObjectID
}

// isObjectFile reports whether the file named by objectID is a regular file of its size.
// A missing file is not an error.
// note: an equal length does not verify the file's hash.
func (repo *Repository) isObjectFile(objectID *astral.ObjectID) (bool, error) {
	stat, err := os.Stat(filepath.Join(repo.root, objectID.String()))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	}

	return stat.Mode().IsRegular() && uint64(stat.Size()) == objectID.Size, nil
}

// refreshIndexLocked replaces the index with a listing of the root directory. The caller holds mu.
// why: the index is published only after a complete listing, so a failed or cancelled refresh keeps the previous one.
func (repo *Repository) refreshIndexLocked(ctx *astral.Context) error {
	entries, err := os.ReadDir(repo.root)
	if err != nil {
		return err
	}

	var ids = &sig.Map[[32]byte, []astral.ObjectID]{}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return err
		}

		objectID := entryObjectID(entry)
		if objectID == nil {
			continue
		}

		candidates, _ := ids.Get(objectID.Hash)
		ids.Replace(objectID.Hash, append(candidates, *objectID))
	}

	repo.ids = ids

	return nil
}

// addObjectLocked adds a committed object to the index. The caller holds mu.
// why: an index not built yet stays nil, so the first partial lookup lists every file already stored.
func (repo *Repository) addObjectLocked(objectID *astral.ObjectID) {
	if repo.ids == nil {
		return
	}

	candidates, _ := repo.ids.Get(objectID.Hash)
	if slices.Contains(candidates, *objectID) {
		return
	}

	repo.ids.Replace(objectID.Hash, append(slices.Clone(candidates), *objectID))
}

// evict removes objectID from the index.
func (repo *Repository) evict(objectID *astral.ObjectID) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.evictLocked(objectID)
}

// evictLocked removes objectID from the index. The caller holds mu.
func (repo *Repository) evictLocked(objectID *astral.ObjectID) {
	if repo.ids == nil {
		return
	}

	candidates, _ := repo.ids.Get(objectID.Hash)
	repo.setCandidatesLocked(objectID.Hash, slices.DeleteFunc(slices.Clone(candidates), func(candidate astral.ObjectID) bool {
		return candidate == *objectID
	}))
}

// setCandidatesLocked replaces the candidates of the hash. The caller holds mu.
func (repo *Repository) setCandidatesLocked(hash [32]byte, candidates []astral.ObjectID) {
	if len(candidates) == 0 {
		repo.ids.Delete(hash)
		return
	}

	repo.ids.Replace(hash, candidates)
}
