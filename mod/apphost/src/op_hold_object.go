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
		return mod.holdObjectBatch(ctx, ch, q.Caller(), args.Duration)
	}

	return ch.Send(mod.holdOne(q.Caller(), args.ID, args.Duration))
}

// holdObjectBatch reads object IDs from ch until EOS/EOF and holds each one
// for caller; a failed input does not stop the batch. An explicit EOS is
// answered with an EOS reply.
func (mod *Module) holdObjectBatch(ctx *astral.Context, ch *channel.Channel, caller *astral.Identity, duration *astral.Duration) error {
	var sawEOS bool
	err := ch.Switch(
		func(id *astral.ObjectID) error {
			return ch.Send(mod.holdOne(caller, id, duration))
		},
		// why: a composed upstream op reports a failed item as a wrong-typed
		// object in the stream; reply in-band and keep the batch alive.
		func(obj astral.Object) error {
			return ch.Send(astral.Err(astral.NewErrUnexpectedObject(obj)))
		},
		func(*astral.EOS) error {
			sawEOS = true
			return channel.ErrBreak
		},
		channel.WithContext(ctx),
	)
	// why: no EOS reply after EOF — the caller is gone and Conn closes on read error.
	if err != nil || !sawEOS {
		return err
	}
	return ch.Send(&astral.EOS{})
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
