package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opWriteArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
}

// OpWrite creates or replaces a file. It answers ack when the writer is open, receives blobs until a
// mod.objects.commit_msg, commits the content and answers the entry. A stream that ends before the commit message
// changes nothing. See VFS 6, sections 7 and 10.
func (mod *Module) OpWrite(ctx *astral.Context, q *routing.IncomingQuery, args opWriteArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessWrite)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: is a folder and not a folder are answered before the writer opens (operator decision 2026-11-12).
	allowed, err := mod.computeAllowance(vol, names)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	w, err := mod.openWriter(ctx)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	defer w.Discard()

	if err = ch.Send(&astral.Ack{}); err != nil {
		return err
	}

	id, err := receiveContent(ctx, ch, w, allowed)
	switch {
	case err != nil:
		return ch.Send(astral.Err(err))
	case id == nil:
		// note: the stream ended before the commit message; nothing changes.
		return nil
	}

	row, err := mod.recordFile(vol, names, *id)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(row.toEntry())
}
