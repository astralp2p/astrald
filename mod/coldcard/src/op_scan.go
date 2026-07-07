package coldcard

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opScanArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpScan triggers a device rescan and replies over the channel with an Ack on
// success or an Error.
func (mod *Module) OpScan(ctx *astral.Context, q *routing.IncomingQuery, args opScanArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.Scan()
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
