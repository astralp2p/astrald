package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opIndexArgs struct {
	ID  *astral.ObjectID `query:"optional"` // batch mode when omitted
	In  string           `query:"optional"`
	Out string           `query:"optional"`
}

// OpIndex handles the index remote operation. With args.ID set it loads that
// SignedContract, indexes it into the local DB and replies Ack or an error
// frame. Without args.ID the op runs in batch mode: object IDs are read from
// the channel until EOS and each input gets one Ack/ErrorMessage reply.
func (mod *Module) OpIndex(ctx *astral.Context, q *routing.IncomingQuery, args opIndexArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.ID == nil {
		return mod.indexBatch(ctx, ch)
	}

	return ch.Send(mod.indexOne(ctx, args.ID))
}

// indexOne loads, verifies and indexes one contract and returns the reply
// object — Ack on success, ErrorMessage on failure.
func (mod *Module) indexOne(ctx *astral.Context, id *astral.ObjectID) astral.Object {
	object, err := mod.Objects.Load(ctx, mod.Objects.ReadDefault(), id)
	if err != nil {
		return astral.Err(err)
	}

	signed, ok := object.(*auth.SignedContract)
	if !ok {
		return astral.Err(auth.ErrInvalidContract)
	}

	if err := mod.IndexContract(ctx, signed); err != nil {
		return astral.Err(err)
	}

	return &astral.Ack{}
}

// indexBatch reads object IDs from ch until EOS/EOF and indexes each one; a
// failed input does not stop the batch. An explicit EOS is answered with an
// EOS reply.
func (mod *Module) indexBatch(ctx *astral.Context, ch *channel.Channel) error {
	var sawEOS bool
	err := ch.Switch(
		func(id *astral.ObjectID) error {
			return ch.Send(mod.indexOne(ctx, id))
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
