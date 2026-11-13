package vfs

import (
	"errors"

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

	return mod.runLockedTx(id, func(tx *DB) error {
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

// setLimits writes the limits of a VFS, or returns vfsapi.ErrNoSuchVFS.
//
// note: the lock is taken before the existence check.
func (mod *Module) setLimits(id *astral.Identity, byteLimit, entryLimit uint64) error {
	return mod.runLockedTx(id, func(tx *DB) error {
		vol, err := tx.findVolume(id)
		if err != nil {
			return err
		}

		return tx.setVolumeLimits(vol.ID, byteLimit, entryLimit)
	})
}
