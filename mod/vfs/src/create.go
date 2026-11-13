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
// Any non-zero identity is accepted, the node's included. The zero identity is refused with
// vfsapi.ErrNotPermitted.
//
// note: operator decision 2026-11-13: the node's identity is accepted; no automatic path creates a VFS for the node.
func (mod *Module) CreateVFS(id *astral.Identity) error {
	if id.IsZero() {
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
