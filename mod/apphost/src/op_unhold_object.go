package apphost

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

type opUnholdObjectArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

func (mod *Module) OpUnholdObject(ctx *astral.Context, q *routing.IncomingQuery, args opUnholdObjectArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	if args.ID == nil || args.ID.IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingObjectID))
	}

	if err := mod.db.UnholdObject(q.Caller(), args.ID); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
