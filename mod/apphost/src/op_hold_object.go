package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opHoldObjectArgs struct {
	ID       *astral.ObjectID `query:"optional"` // batch mode when omitted
	Duration *astral.Duration `query:"optional"`
	In       string           `query:"optional"`
	Out      string           `query:"optional"`
}

// OpHoldObject handles the hold_object operation. With args.ID set it places
// one hold and replies Ack or an error frame. Without args.ID the op runs in
// batch mode: object IDs are read from the channel until EOS and each input
// gets one Ack/ErrorMessage reply.
func (mod *Module) OpHoldObject(ctx *astral.Context, q *routing.IncomingQuery, args opHoldObjectArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	if args.ID == nil {
		return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
			return mod.holdOne(q.Caller(), id, args.Duration)
		}, channel.WithContext(ctx))
	}

	return ch.Send(mod.holdOne(q.Caller(), args.ID, args.Duration))
}

// holdOne places one hold and returns the reply object — Ack on success,
// ErrorMessage on failure.
func (mod *Module) holdOne(caller *astral.Identity, id *astral.ObjectID, duration *astral.Duration) astral.Object {
	if id.IsZero() {
		return astral.Err(apphost.ErrMissingObjectID)
	}

	if err := mod.db.HoldObject(caller, id, duration); err != nil {
		return astral.Err(err)
	}

	return &astral.Ack{}
}
