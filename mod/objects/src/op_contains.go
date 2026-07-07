package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opContainsArgs struct {
	Repo string
	ID   *astral.ObjectID `query:"optional"`
	In   string           `query:"optional"`
	Out  string           `query:"optional"`
}

// OpContains reports whether a repository holds an object. With the ID arg it
// answers once; otherwise it streams a Bool per ObjectID read from the channel.
func (mod *Module) OpContains(ctx *astral.Context, q *routing.IncomingQuery, args opContainsArgs) (err error) {
	ctx = ctx.WithIdentity(q.Caller())

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.NewError("repository not found"))
	}

	if args.ID != nil {
		has, err := repo.Contains(ctx, args.ID)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}

		return ch.Send((*astral.Bool)(&has))
	}

	return ch.Handle(ctx, func(object astral.Object) {
		switch object := object.(type) {
		case *astral.ObjectID:
			has, err := repo.Contains(ctx, object)
			if err != nil {
				ch.Send(astral.NewError(err.Error()))
			} else {
				ch.Send((*astral.Bool)(&has))
			}

		case *astral.EOS:
			//ignore

		default:
			ch.Send(astral.NewErrUnexpectedObject(object)) // ignore
		}
	})

}
