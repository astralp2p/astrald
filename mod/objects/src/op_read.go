package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opReadArgs struct {
	ID     *astral.ObjectID
	Offset astral.Uint64 `query:"optional"`
	Limit  astral.Uint64 `query:"optional"`
	Zone   astral.Zone   `query:"optional"`
	Repo   string        `query:"optional"`
}

// OpRead authorizes the caller, then streams raw object bytes over the accepted
// connection. Records the access in the reads journal, which feeds purge ordering.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (err error) {
	ctx = ctx.IncludeZone(args.Zone)
	repo := mod.ReadDefault()

	allowed := mod.Auth.Authorize(ctx, &objects.ReadObjectAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	})
	if !allowed {
		return q.Reject()
	}

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

	mod.objectsReadsJournal.Mark(args.ID)

	conn := q.AcceptRaw()
	defer conn.Close()

	_, err = io.Copy(conn, r)

	return err
}
