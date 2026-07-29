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
		return channel.Batch(ch, func(id *astral.ObjectID) astral.Object {
			return mod.indexOne(ctx, id)
		}, channel.WithContext(ctx))
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
