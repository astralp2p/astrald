package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// filledID is the ObjectID computed for the folder in one row.
type filledID struct {
	Row int64
	ID  astral.ObjectID
}

// clearFolderIDs clears the cached ObjectID of a folder and of every folder above it, up to the root.
//
// A change in a folder, or below it, calls this with the row of the folder.
func (db *DB) clearFolderIDs(folderID int64) error {
	// note: the root is its own parent, and UNION drops a repeated row, so the recursion ends there.
	return db.Exec(`
		UPDATE vfs__entries SET object_id = NULL WHERE id IN (
			WITH RECURSIVE ancestors(id, parent_id) AS (
				SELECT id, parent_id FROM vfs__entries WHERE id = ?
				UNION
				SELECT e.id, e.parent_id FROM vfs__entries e JOIN ancestors a ON e.id = a.parent_id
			)
			SELECT id FROM ancestors
		)`, folderID).Error
}

// computeFolderID returns the ObjectID of the listing of a folder, computed bottom-up: the folders below
// first, then the folder. A folder with a cached ObjectID is not read again.
//
// The call only reads, so it belongs in one read transaction. It also returns the IDs it filled in, for
// storeFolderIDs to cache.
func (db *DB) computeFolderID(folderID int64) (id astral.ObjectID, filled []filledID, err error) {
	row, err := db.getEntry(folderID)
	if err != nil {
		return id, nil, err
	}

	return db.computeFolder(row, nil)
}

func (db *DB) computeFolder(row *dbEntry, filled []filledID) (astral.ObjectID, []filledID, error) {
	if row.ObjectID != nil {
		return *row.ObjectID, filled, nil
	}

	children, err := db.listChildren(row.ID)
	if err != nil {
		return astral.ObjectID{}, nil, err
	}

	listing, err := vfsapi.NewListingWriter(nil, uint32(len(children)))
	if err != nil {
		return astral.ObjectID{}, nil, err
	}

	for _, child := range children {
		var childID astral.ObjectID

		if child.isFolder() {
			if childID, filled, err = db.computeFolder(child, filled); err != nil {
				return astral.ObjectID{}, nil, err
			}
			child.ObjectID = &childID
		}

		if err = listing.Write(child.entry()); err != nil {
			return astral.ObjectID{}, nil, err
		}
	}

	id, err := listing.Close()
	if err != nil {
		return astral.ObjectID{}, nil, err
	}

	return *id, append(filled, filledID{Row: row.ID, ID: *id}), nil
}

// storeFolderIDs caches computed ObjectIDs in their rows.
//
// The caller holds the lock of the VFS, so no change cleared a folder between the computation and the store.
func (db *DB) storeFolderIDs(filled []filledID) error {
	for _, f := range filled {
		id := f.ID
		if err := db.Model(&dbEntry{}).Where("id = ?", f.Row).Update("object_id", &id).Error; err != nil {
			return err
		}
	}

	return nil
}

// folderObjectID returns the ObjectID of the listing of a folder, computing and caching it when it is cleared.
// It takes the lock of the VFS only when it has to compute, so a caller must not hold that lock.
//
// A caller inside a transaction under the lock calls computeFolderID and storeFolderIDs on its own transaction.
func (mod *Module) folderObjectID(vfsID *astral.Identity, folderID int64) (astral.ObjectID, error) {
	row, err := mod.db.getEntry(folderID)
	if err != nil {
		return astral.ObjectID{}, err
	}
	if row.ObjectID != nil {
		return *row.ObjectID, nil
	}

	unlock := mod.lockVFS(vfsID)
	defer unlock()

	var id astral.ObjectID

	err = mod.db.inTx(func(tx *DB) (err error) {
		var filled []filledID

		if id, filled, err = tx.computeFolderID(folderID); err != nil {
			return err
		}

		return tx.storeFolderIDs(filled)
	})

	return id, err
}
