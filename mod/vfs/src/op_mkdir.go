package vfs

import (
	"errors"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMkdirArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
}

// OpMkdir creates a folder and its missing parents, and answers ack. An existing folder succeeds; a file at the
// path answers vfsapi.ErrAlreadyExists. See VFS 6, section 7.
func (mod *Module) OpMkdir(ctx *astral.Context, q *routing.IncomingQuery, args opMkdirArgs) error {
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

	err = mod.changeVFS(vol, func(tx *DB) error { return tx.mkdirCreate(vol, names) })
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// mkdirCreate creates the folder the names give, with its missing parents.
func (db *DB) mkdirCreate(vol *dbVolume, names []string) error {
	// note: the root always exists.
	if len(names) == 0 {
		return nil
	}

	before, err := db.changeMeasure(vol.Identity)
	if err != nil {
		return err
	}

	parent, err := db.changeMakeFolders(vol, names[:len(names)-1])
	if err != nil {
		return err
	}

	row, err := db.findChild(parent.ID, names[len(names)-1])
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		row = &dbEntry{Name: names[len(names)-1], Flags: uint16(vfsapi.FlagFolder)}
		if err = db.changeAddEntry(parent, row); err != nil {
			return err
		}
	case err != nil:
		return err
	case !row.isFolder():
		return vfsapi.ErrAlreadyExists
	}

	return db.changeCheckLimits(vol.Identity, before)
}
