package objects

import (
	"errors"
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
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
// A refusal rejects with astral.CodeRejected, and an object no repository could
// supply rejects with objectsmod.CodeUnavailable.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) (err error) {
	ctx = ctx.IncludeZone(args.Zone)

	// why: a read from the network answers from this node alone. A network
	// repository would forward it to a sibling, and that sibling's repository
	// back again.
	if q.Origin() == astral.OriginNetwork {
		ctx = ctx.ExcludeZone(astral.ZoneNetwork)
	}

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
		// why: a group, the siblings repository among its members, reports every miss
		// and every failed sibling retrieval as ErrNotFound.
		if errors.Is(err, objectsmod.ErrNotFound) {
			return q.RejectWithCode(objectsmod.CodeUnavailable)
		}
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
