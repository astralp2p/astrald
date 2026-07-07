package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opDeleteArgs struct {
	ID   *astral.ObjectID `query:"optional"`
	Repo string
	Out  string       `query:"optional"`
	Zone *astral.Zone `query:"optional"`
}

// OpDelete deletes one object (ID arg) or a stream of objects read from the
// channel. Requires an explicit repository — there is no default, to avoid
// accidental deletion.
func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteArgs) (err error) {
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

	// otherwise read objects ids from the channel
	return ch.Handle(ctx, func(object astral.Object) {
		switch object := object.(type) {
		case *astral.ObjectID:
			err := repo.Delete(ctx, object)
			if err != nil {
				ch.Send(astral.NewError(err.Error()))
			} else {
				ch.Send(&astral.Ack{})
			}

		case *astral.Ack,
			*astral.EOS:

		default: // protocol error
			ch.Send(astral.NewError("protocol error"))
			ch.Close()
		}
	})

}
