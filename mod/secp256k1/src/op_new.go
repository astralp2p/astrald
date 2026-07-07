package secp256k1

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type opNewArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpNew generates a fresh secp256k1 key and sends it over the accepted channel.
func (mod *Module) OpNew(ctx *astral.Context, q *routing.IncomingQuery, args opNewArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Send(secp256k1.New())
}
