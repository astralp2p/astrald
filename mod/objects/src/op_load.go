package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opLoadArgs struct {
	ID       *astral.ObjectID
	Unparsed bool
	Repo     string
	Zone     *astral.Zone
	Out      string
}

// OpLoad loads an object into memory and writes it to the output. OpLoad verifies the object hash.
func (mod *Module) OpLoad(ctx *astral.Context, q *routing.IncomingQuery, args opLoadArgs) (err error) {
	if !mod.authorizeSeeObjects(ctx, q, args.ID, args.Repo) {
		return q.Reject()
	}

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

	return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
		o, err := mod.Load(ctx, repo, id)
		if err != nil {
			return astral.NewError(err.Error())
		}
		return o
	}, channel.WithContext(ctx))
}
