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

// folderIDComputer computes the ObjectIDs of folder listings bottom-up: the folders below first, then the
// folder. It records in filled each ID it computed, for setFolderIDs to cache.
//
// note: it only reads, so it belongs in one read transaction.
type folderIDComputer struct {
	tx     *DB
	filled []filledID
}

// compute returns the ObjectID of the listing of the folder in row. A folder with a cached ObjectID is not
// read again.
func (fc *folderIDComputer) compute(row *dbEntry) (astral.ObjectID, error) {
	if row.ObjectID != nil {
		return *row.ObjectID, nil
	}

	children, err := fc.tx.listChildren(row.ID)
	if err != nil {
		return astral.ObjectID{}, err
	}

	listing, err := vfsapi.NewListingWriter(nil, uint32(len(children)))
	if err != nil {
		return astral.ObjectID{}, err
	}

	for _, child := range children {
		if child.isFolder() {
			childID, err := fc.compute(child)
			if err != nil {
				return astral.ObjectID{}, err
			}
			child.ObjectID = &childID
		}

		if err = listing.Write(child.toEntry()); err != nil {
			return astral.ObjectID{}, err
		}
	}

	id, err := listing.Close()
	if err != nil {
		return astral.ObjectID{}, err
	}

	fc.filled = append(fc.filled, filledID{Row: row.ID, ID: *id})

	return *id, nil
}

// fillFolderID computes and caches the ObjectID of a folder whose ObjectID is cleared, and sets it on row.
// Any other row is left alone.
//
// note: the caller holds the lock of the VFS and runs inside a transaction.
func fillFolderID(tx *DB, row *dbEntry) error {
	if !row.isFolder() || row.ObjectID != nil {
		return nil
	}

	fc := folderIDComputer{tx: tx}

	id, err := fc.compute(row)
	if err != nil {
		return err
	}

	if err = tx.setFolderIDs(fc.filled); err != nil {
		return err
	}

	row.ObjectID = &id

	return nil
}

// folderObjectID returns the ObjectID of the listing of a folder, computing and caching it when it is cleared.
// It takes the lock of the VFS only when it has to compute, so a caller must not hold that lock.
//
// A caller inside a transaction under the lock calls fillFolderID on its own transaction.
func (mod *Module) folderObjectID(vfsID *astral.Identity, folderID int64) (astral.ObjectID, error) {
	row, err := mod.db.findEntry(folderID)
	if err != nil {
		return astral.ObjectID{}, err
	}
	if row.ObjectID != nil {
		return *row.ObjectID, nil
	}

	var id astral.ObjectID

	err = mod.runLockedTx(vfsID, func(tx *DB) error {
		row, err := tx.findEntry(folderID)
		if err != nil {
			return err
		}

		if err = fillFolderID(tx, row); err != nil {
			return err
		}

		id = *row.ObjectID

		return nil
	})

	return id, err
}
