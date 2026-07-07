package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opLoadArgs struct {
	ID       *astral.ObjectID `query:"optional"`
	Unparsed bool             `query:"optional"`
	Repo     string           `query:"optional"`
	Zone     *astral.Zone     `query:"optional"`
	Out      string           `query:"optional"`
}

// OpLoad loads an object into memory and writes it to the output. OpLoad verifies the object hash.
func (mod *Module) OpLoad(ctx *astral.Context, q *routing.IncomingQuery, args opLoadArgs) (err error) {
	if args.Zone == nil {
		ctx = ctx.WithZone(astral.ZoneAll)
	} else {
		ctx = ctx.WithZone(*args.Zone)
	}

	ch := q.Accept(channel.WithOutputFormat(args.Out), channel.AllowUnparsed(args.Unparsed))
	defer ch.Close()

	repo := mod.ReadDefault()
	if len(args.Repo) > 0 {
		repo = mod.GetRepository(args.Repo)
		if repo == nil {
			return ch.Send(astral.NewError("repository not found"))
		}
	}

	// if an ID was provided, load a single object
	if args.ID != nil {
		o, err := mod.Load(ctx, repo, args.ID)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}

		return ch.Send(o)
	}

	return ch.Handle(ctx, func(object astral.Object) {
		switch object := object.(type) {
		case *astral.ObjectID:
			o, err := mod.Load(ctx, repo, object)
			if err != nil {
				ch.Send(astral.NewError(err.Error()))
			} else {
				ch.Send(o)
			}

		case *astral.EOS:
			//ignore

		default:
			ch.Send(astral.NewErrUnexpectedObject(object))
		}
	})
}
