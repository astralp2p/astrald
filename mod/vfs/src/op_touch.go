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

	vol, err := mod.changeAuthorize(ctx, q, args.VFS)
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

	if !touchEncodable(modTime) {
		return ch.Send(astral.Err(vfsapi.ErrInvalidTime))
	}

	var entry *vfsapi.Entry

	err = mod.changeVFS(vol, func(tx *DB) (err error) {
		entry, err = tx.touchEntry(vol, names, modTimeColumn(modTime))
		return err
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(entry)
}

// touchEncodable reports whether the time type encodes a time: whole nanoseconds since the Unix epoch in a uint64,
// within the int64 range that time.Time converts without loss.
func touchEncodable(t time.Time) bool {
	ns := t.UnixNano()

	return ns >= 0 && time.Unix(0, ns).Equal(t)
}

// touchEntry sets the time of the entry the names give.
//
// note: the time of a folder is outside its own listing, so only the folders above it lose their cached IDs.
func (db *DB) touchEntry(vol *dbVolume, names []string, modTime int64) (*vfsapi.Entry, error) {
	// why: the root cannot be touched (operator decision 2026-11-12).
	if len(names) == 0 {
		return nil, vfsapi.ErrInvalidPath
	}

	row, err := db.resolveEntry(vol, names)
	if err != nil {
		return nil, err
	}

	if err = db.setEntryTime(row.ID, modTime); err != nil {
		return nil, err
	}
	row.ModTime = modTime

	if err = db.clearFolderIDs(row.ParentID); err != nil {
		return nil, err
	}

	return db.changeEntry(row)
}
