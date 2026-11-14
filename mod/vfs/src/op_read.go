package vfs

import (
	"math"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opReadArgs struct {
	VFS    *astral.Identity
	Path   string `query:"required"`
	Offset uint64
	Limit  uint64
}

// OpRead streams the raw bytes of a file (VFS 6, section 7).
//
// why: the answer is raw bytes with no framing, so every failure rejects the query.
// note: the content comes from the local group only, with the zone narrowed to the device zone. Content that is
// missing is a failure, and no network lookup replaces it.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) error {
	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessRead)
	if err != nil {
		return q.Reject()
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return q.Reject()
	}

	id, err := mod.findFileID(vol, names)
	if err != nil || args.Offset > id.Size {
		return q.Reject()
	}

	repo := mod.Objects.GetRepository(readRepository)
	if repo == nil {
		return q.Reject()
	}

	// note: an offset equal to the size is a valid window of zero bytes, and a limit of 0 reads to the end.
	r, err := repo.Read(ctx.LimitZone(astral.ZoneDevice), &id, int64(args.Offset), int64(min(args.Limit, math.MaxInt64)))
	if err != nil {
		mod.log.Logv(2, "read %v: %v", id, err)
		return q.Reject()
	}
	defer r.Close()

	return answerRaw(q, r)
}
