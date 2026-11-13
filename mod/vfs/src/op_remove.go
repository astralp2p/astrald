package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveArgs struct {
	In        string
	Out       string
	VFS       *astral.Identity
	Path      string `query:"required"`
	Recursive bool
}

// OpRemove removes an entry and answers ack. A folder with entries needs recursive. The content of a removed file
// stays in the repository, unheld, until purge. See VFS 6, section 7.
func (mod *Module) OpRemove(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.changeAuthorize(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.changeVFS(vol, func(tx *DB) error { return tx.removeEntry(vol, names, args.Recursive) })
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// removeEntry removes the entry the names give, with everything below it when recursive is set.
func (db *DB) removeEntry(vol *dbVolume, names []string, recursive bool) error {
	// why: the root cannot be removed (operator decision 2026-11-12).
	if len(names) == 0 {
		return vfsapi.ErrInvalidPath
	}

	row, err := db.resolveEntry(vol, names)
	if err != nil {
		return err
	}

	if row.isFolder() && !recursive {
		var n int64
		if err = db.Model(&dbEntry{}).Where("parent_id = ?", row.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return vfsapi.ErrFolderNotEmpty
		}
	}

	if err = db.removeTree(row.ID); err != nil {
		return err
	}

	if err = db.changeSetTime(row.ParentID, changeNow()); err != nil {
		return err
	}

	return db.clearFolderIDs(row.ParentID)
}

// removeTree deletes a row and every row below it.
//
// note: the walk goes down from a row that is not a root, so it never reaches the root, which is its own parent.
func (db *DB) removeTree(rowID int64) error {
	return db.Exec(`
		DELETE FROM vfs__entries WHERE id IN (
			WITH RECURSIVE tree(id) AS (
				SELECT id FROM vfs__entries WHERE id = ?
				UNION
				SELECT e.id FROM vfs__entries e JOIN tree t ON e.parent_id = t.id
			)
			SELECT id FROM tree
		)`, rowID).Error
}
