package apphost

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

type opListHeldObjectsArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpListHeldObjects(ctx *astral.Context, q *routing.IncomingQuery, args opListHeldObjectsArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	rows, err := mod.db.ListHeldObjects(q.Caller())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, row := range rows {
		if err := ch.Send(row.ObjectID); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
