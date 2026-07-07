package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opBlueprintsArgs struct {
	Out string `query:"optional"`
}

// OpBlueprints streams every registered type name in dependency order
// (compile-time prototypes first, then runtime Blueprints topo-sorted by
// reference). Each name is sent as String8; the stream is terminated by an
// astral.EOS marker so consumers can iterate without relying on channel
// close to signal end-of-stream.
func (mod *Module) OpBlueprints(ctx *astral.Context, q *routing.IncomingQuery, args opBlueprintsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, name := range astral.DefaultBlueprints().OrderedBlueprints() {
		if err = ch.Send((*astral.String8)(&name)); err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
