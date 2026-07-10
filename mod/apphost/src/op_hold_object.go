package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opHoldObjectArgs struct {
	ID       *astral.ObjectID
	Duration *astral.Duration `query:"optional"`
	Out      string           `query:"optional"`
}

func (mod *Module) OpHoldObject(ctx *astral.Context, q *routing.IncomingQuery, args opHoldObjectArgs) error {
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

	err := mod.db.HoldObject(q.Caller(), args.ID, args.Duration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
