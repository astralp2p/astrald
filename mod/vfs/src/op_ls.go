package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opLsArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path *string
}

// OpLs streams the entries of a folder in ascending byte order of Name, then eos (VFS 6, section 7).
func (mod *Module) OpLs(ctx *astral.Context, q *routing.IncomingQuery, args opLsArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.readTarget(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	path := "/"
	if args.Path != nil {
		path = *args.Path
	}

	rows, err := mod.lsRows(vol, path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, row := range rows {
		entry, err := mod.readEntry(vol, row)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		if err = ch.Send(entry); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

// lsRows returns the children of the folder at a path, in one read transaction.
func (mod *Module) lsRows(vol *dbVolume, path string) (rows []*dbEntry, err error) {
	names, err := parsePath(path)
	if err != nil {
		return nil, err
	}

	err = mod.db.inTx(func(tx *DB) error {
		folder, err := tx.resolveEntry(vol, names)
		if err != nil {
			return err
		}
		if !folder.isFolder() {
			return vfsapi.ErrNotAFolder
		}

		rows, err = tx.listChildren(folder.ID)

		return err
	})

	return rows, err
}
