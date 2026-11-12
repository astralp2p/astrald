package vfs

import (
	"errors"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// CreateVFS creates the VFS of an identity with the default quotas, and its root folder. A VFS that exists is
// left as it is.
//
// The zero identity is refused with vfsapi.ErrNotPermitted.
//
// note: provisional, pending operator decision: does vfs.create accept the identity of the node? VFS 6 section 7
// accepts any non-zero identity and section 6 says no VFS is created for the node, so the node is refused.
func (mod *Module) CreateVFS(id *astral.Identity) error {
	if id.IsZero() || id.IsEqual(mod.identity) {
		return vfsapi.ErrNotPermitted
	}

	unlock := mod.lockVFS(id)
	defer unlock()

	return mod.db.inTx(func(tx *DB) error {
		_, err := tx.findVolume(id)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, vfsapi.ErrNoSuchVFS):
		default:
			return err
		}

		return tx.createVolume(id, mod.config.ByteLimit, mod.config.EntryLimit)
	})
}

// createVolume inserts a VFS with its root folder. The root is its own parent.
func (db *DB) createVolume(id *astral.Identity, byteLimit, entryLimit uint64) error {
	root := &dbEntry{
		VFS:     id,
		Flags:   uint16(vfsapi.FlagFolder),
		ModTime: modTimeColumn(time.Now()),
	}

	if err := db.Create(root).Error; err != nil {
		return err
	}

	root.ParentID = root.ID
	if err := db.Model(root).Update("parent_id", root.ID).Error; err != nil {
		return err
	}

	return db.Create(&dbVolume{
		Identity:   id,
		RootID:     root.ID,
		ByteLimit:  limitColumn(byteLimit),
		EntryLimit: limitColumn(entryLimit),
	}).Error
}
