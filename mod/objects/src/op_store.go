package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opStoreArgs struct {
	Repo string
	In   string
	Out  string
}

func (mod *Module) OpStore(ctx *astral.Context, q *routing.IncomingQuery, args opStoreArgs) error {
	ch := channel.New(
		q.AcceptRaw(),
		channel.WithFormats(args.In, args.Out),
		channel.AllowUnparsed(true), // allow unparsed objects
	)
	defer ch.Close()

	repo := mod.WriteDefault()
	if len(args.Repo) > 0 {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return ch.Send(astral.NewError("repository not found"))
		}
	}

	return channel.Batch(ch, func(object astral.Object) astral.Object {
		objectID, err := mod.Store(ctx, repo, object)
		if err != nil {
			return astral.NewError(err.Error())
		}
		return objectID
	}, channel.WithContext(ctx))
}
