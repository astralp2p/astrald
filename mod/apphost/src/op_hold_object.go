package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opHoldObjectArgs struct {
	ID       *astral.ObjectID // batch mode when omitted
	Duration *astral.Duration
	In       string
	Out      string
}

// OpHoldObject handles the hold_object operation. With args.ID set it places
// one hold and replies Ack or an error frame. Without args.ID the op runs in
// batch mode: object IDs are read from the channel until EOS and each input
// gets one Ack/ErrorMessage reply.
func (mod *Module) OpHoldObject(ctx *astral.Context, q *routing.IncomingQuery, args opHoldObjectArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if q.Caller().IsZero() {
		return ch.Send(astral.Err(apphost.ErrMissingAppIdentity))
	}

	// why: a hold is a write to node state that objects.purge obeys, so it asks the
	// write authority. Batch-mode IDs arrive after the decision, so no noun is named.
	if !mod.Auth.Authorize(ctx, &auth.StoreObjectsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
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

	if id.Size == 0 {
		return astral.Err(astral.ErrPartialObjectID)
	}

	if err := mod.db.HoldObject(caller, id, duration); err != nil {
		return astral.Err(err)
	}

	return &astral.Ack{}
}
