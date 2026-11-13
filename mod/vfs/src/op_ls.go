package vfs

import (
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

	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessRead)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	path := "/"
	if args.Path != nil {
		path = *args.Path
	}

	names, err := parsePath(path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	rows, err := mod.listFolder(vol, names)
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
