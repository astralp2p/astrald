package objects

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opReadArgs struct {
	ID     *astral.ObjectID `query:"required"`
	Offset astral.Uint64
	Limit  astral.Uint64
	Zone   astral.Zone
	Repo   string
}

// OpRead authorizes the caller under SeeObjects, then streams raw object bytes
// over the accepted connection. Records the access under the opened object's
// full ID in the reads journal, which feeds purge ordering.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (err error) {
	ctx = ctx.IncludeZone(args.Zone)

	if !mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
		Repo:     astral.String8(args.Repo),
	}) {
		return q.Reject()
	}

	repo := mod.ReadDefault()

	if len(args.Repo) > 0 {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return q.Reject()
		}
	}

	r, err := repo.Read(
		ctx.WithIdentity(q.Caller()),
		args.ID,
		int64(args.Offset),
		int64(args.Limit),
	)
	if err != nil {
		mod.log.Errorv(2, "read %v error: %v", args.ID, err)
		return q.Reject()
	}
	defer r.Close()

	// why: a partial request never becomes a journal key; the reader names the object it opened.
	mod.objectsReadsJournal.Mark(r.ID())

	conn := q.AcceptRaw()
	defer conn.Close()

	_, err = io.Copy(conn, r)

	return err
}
