package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opUnholdObjectArgs struct {
	ID  *astral.ObjectID `query:"optional"` // batch mode when omitted
	In  string           `query:"optional"`
	Out string           `query:"optional"`
}

// OpUnholdObject handles the unhold_object operation. With args.ID set it
// releases one hold and replies Ack or an error frame. Without args.ID the op
// runs in batch mode: object IDs are read from the channel until EOS and each
// input gets one Ack/ErrorMessage reply.
func (mod *Module) OpUnholdObject(ctx *astral.Context, q *routing.IncomingQuery, args opUnholdObjectArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	if args.ID == nil {
		return mod.unholdObjectBatch(ctx, ch, q.Caller())
	}

	return ch.Send(mod.unholdOne(q.Caller(), args.ID))
}

// unholdObjectBatch reads object IDs from ch until EOS/EOF and releases each
// hold for caller; a failed input does not stop the batch. An explicit EOS is
// answered with an EOS reply.
func (mod *Module) unholdObjectBatch(ctx *astral.Context, ch *channel.Channel, caller *astral.Identity) error {
	var sawEOS bool
	err := ch.Switch(
		func(id *astral.ObjectID) error {
			return ch.Send(mod.unholdOne(caller, id))
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

// unholdOne releases one hold and returns the reply object — Ack on success,
// ErrorMessage on failure. Releasing a hold that does not exist is a no-op Ack.
func (mod *Module) unholdOne(caller *astral.Identity, id *astral.ObjectID) astral.Object {
	if id.IsZero() {
		return astral.Err(apphost.ErrMissingObjectID)
	}

	if err := mod.db.UnholdObject(caller, id); err != nil {
		return astral.Err(err)
	}

	return &astral.Ack{}
}
