package vfs

import (
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opTouchArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
	Time *astral.Time
}

// OpTouch sets the modification time of a file or a folder and answers the touched entry. The time defaults to the
// node's time. Nothing is created. See VFS 6, section 7.
func (mod *Module) OpTouch(ctx *astral.Context, q *routing.IncomingQuery, args opTouchArgs) error {
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

	modTime := time.Now()
	if args.Time != nil {
		modTime = args.Time.Time()
	}

	if !encodableTime(modTime) {
		return ch.Send(astral.Err(vfsapi.ErrInvalidTime))
	}

	var entry *vfsapi.Entry

	err = mod.changeVFS(vol, func(c *change) (err error) {
		entry, err = c.touchEntry(names, modTimeColumn(modTime))
		return err
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(entry)
}
