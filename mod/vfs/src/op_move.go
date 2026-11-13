package vfs

import (
	"errors"
	"slices"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMoveArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	From string `query:"required"`
	To   string `query:"required"`
}

// OpMove moves or renames an entry and answers the moved entry. Missing parents of the target are created, the
// entry keeps its time, and both parent folders get the node's time. See VFS 6, section 7.
func (mod *Module) OpMove(ctx *astral.Context, q *routing.IncomingQuery, args opMoveArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.changeAuthorize(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	from, err := parsePath(args.From)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	to, err := parsePath(args.To)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	var entry *vfsapi.Entry

	err = mod.changeVFS(vol, func(tx *DB) (err error) {
		entry, err = tx.moveEntry(vol, from, to)
		return err
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(entry)
}

// moveEntry moves the entry at from to the path to.
func (db *DB) moveEntry(vol *dbVolume, from, to []string) (*vfsapi.Entry, error) {
	// why: the root cannot be moved (operator decision 2026-11-12).
	// note: operator decision 2026-11-13: a move onto the root answers invalid path, not already exists.
	if len(from) == 0 || len(to) == 0 {
		return nil, vfsapi.ErrInvalidPath
	}

	before, err := db.changeMeasure(vol.Identity)
	if err != nil {
		return nil, err
	}

	row, err := db.resolveEntry(vol, from)
	if err != nil {
		return nil, err
	}

	if row.isFolder() && moveIsBeneath(from, to) {
		return nil, vfsapi.ErrInvalidPath
	}

	parent, err := db.changeMakeFolders(vol, to[:len(to)-1])
	if err != nil {
		return nil, err
	}

	if err = db.moveRelink(row, parent, to[len(to)-1]); err != nil {
		return nil, err
	}

	if err = db.changeCheckLimits(vol.Identity, before); err != nil {
		return nil, err
	}

	return db.changeEntry(row)
}

// moveIsBeneath reports whether the path to is the path from or lies beneath it.
func moveIsBeneath(from, to []string) bool {
	return len(to) >= len(from) && slices.Equal(from, to[:len(from)])
}

// moveRelink gives an entry a new parent and name. Both parents get the node's time and lose their cached IDs.
func (db *DB) moveRelink(row *dbEntry, parent *dbEntry, name string) error {
	_, err := db.findChild(parent.ID, name)
	switch {
	case err == nil:
		return vfsapi.ErrAlreadyExists
	case !errors.Is(err, vfsapi.ErrNotFound):
		return err
	}

	oldParentID := row.ParentID
	row.ParentID, row.Name = parent.ID, name

	err = db.Model(&dbEntry{}).Where("id = ?", row.ID).
		Updates(map[string]any{"parent_id": row.ParentID, "name": row.Name}).Error
	if err != nil {
		return err
	}

	now := changeNow()
	for _, folderID := range []int64{oldParentID, parent.ID} {
		if err = db.changeSetTime(folderID, now); err != nil {
			return err
		}

		if err = db.clearFolderIDs(folderID); err != nil {
			return err
		}
	}

	return nil
}
