package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opFiltersArgs struct {
	In  string
	Out string
}

func (mod *Module) OpFilters(ctx *astral.Context, q *routing.IncomingQuery, args opFiltersArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	for _, f := range mod.Filters() {
		err = ch.Send(astral.NewString8(f))
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
