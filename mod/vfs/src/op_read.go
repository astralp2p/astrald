package vfs

import (
	"io"
	"math"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opReadArgs struct {
	VFS    *astral.Identity
	Path   string `query:"required"`
	Offset uint64
	Limit  uint64
}

// readRepository is the repository group that holds VFS content.
const readRepository = "local"

// OpRead streams the raw bytes of a file (VFS 6, section 7).
//
// why: the answer is raw bytes with no framing, so every failure rejects the query.
// note: the content comes from the local group only, with the zone narrowed to the device zone. Content that is
// missing is a failure, and no network lookup replaces it.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) error {
	vol, err := mod.readTarget(ctx, q, args.VFS)
	if err != nil {
		return q.Reject()
	}

	id, err := mod.readFileID(vol, args.Path)
	if err != nil || args.Offset > id.Size {
		return q.Reject()
	}

	// why: the empty object is never stored, so the answer needs no repository.
	if objectsmod.IsEmptyObject(&id) {
		return mod.readAnswer(q, nil)
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

	return mod.readAnswer(q, r)
}

// readAnswer accepts the query and copies the content to the caller.
func (mod *Module) readAnswer(q *routing.IncomingQuery, content io.Reader) error {
	conn := q.AcceptRaw()
	defer conn.Close()

	if content == nil {
		return nil
	}

	_, err := io.Copy(conn, content)

	return err
}

// readFileID returns the ObjectID of the content of the file at a path. A path that names a folder fails.
func (mod *Module) readFileID(vol *dbVolume, path string) (id astral.ObjectID, err error) {
	names, err := parsePath(path)
	if err != nil {
		return id, err
	}

	row, err := mod.db.resolveEntry(vol, names)
	if err != nil {
		return id, err
	}
	if row.isFolder() {
		return id, vfsapi.ErrIsAFolder
	}
	if row.ObjectID == nil {
		return id, vfsapi.ErrNotFound
	}

	return *row.ObjectID, nil
}
