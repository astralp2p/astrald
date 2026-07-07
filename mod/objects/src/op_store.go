package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opStoreArgs struct {
	Repo string `query:"optional"`
	In   string `query:"optional"`
	Out  string `query:"optional"`
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

	return ch.Switch(
		func(object astral.Object) error {
			objectID, err := mod.Store(ctx, repo, object)
			if err != nil {
				return ch.Send(astral.NewError(err.Error()))
			}

			return ch.Send(objectID)
		},
		channel.BreakOnEOS,
		channel.WithContext(ctx),
	)
}
