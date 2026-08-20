package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDeleteArgs struct {
	ID   *astral.ObjectID
	Repo string `query:"required"`
	Out  string
	Zone *astral.Zone
}

// OpDelete deletes one object (ID arg) or a stream of objects read from the
// channel. Requires an explicit repository — there is no default, to avoid
// accidental deletion.
func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteArgs) (err error) {
	if !mod.authorizeAdminObjects(ctx, q, args.ID, args.Repo) {
		return q.Reject()
	}

	// prepare the context
	ctx = ctx.WithIdentity(q.Caller())
	if args.Zone == nil {
		ctx = ctx.WithZone(astral.ZoneAll)
	} else {
		ctx = ctx.WithZone(*args.Zone)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// look up the repository (no default to avoid accidental deletion)
	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.NewError("repository not found"))
	}

	// if an ID was provided, delete a single object
	if args.ID != nil {
		err := repo.Delete(ctx, args.ID)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
		return ch.Send(&astral.Ack{})
	}

	// otherwise read object ids from the channel
	return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
		if err := repo.Delete(ctx, id); err != nil {
			return astral.NewError(err.Error())
		}
		return &astral.Ack{}
	}, channel.WithContext(ctx))
}
