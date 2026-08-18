package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opContainsArgs struct {
	Repo string `query:"required"`
	ID   *astral.ObjectID
	In   string
	Out  string
}

// OpContains reports whether a repository holds an object. With the ID arg it
// answers once; otherwise it streams a Bool per ObjectID read from the channel.
func (mod *Module) OpContains(ctx *astral.Context, q *routing.IncomingQuery, args opContainsArgs) (err error) {
	if !mod.authorizeSeeObjects(ctx, q, args.ID, args.Repo) {
		return q.Reject()
	}

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

	return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
		has, err := repo.Contains(ctx, id)
		if err != nil {
			return astral.NewError(err.Error())
		}
		return (*astral.Bool)(&has)
	}, channel.WithContext(ctx))
}
