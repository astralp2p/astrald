package dir

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opApplyFiltersArgs struct {
	Filters string `query:"required"`
	ID      string
	In      string
	Out     string
}

func (mod *Module) OpApplyFilters(ctx *astral.Context, q *routing.IncomingQuery, args opApplyFiltersArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// set initial values
	var (
		identity = q.Caller()
		filters  = strings.Split(args.Filters, ",")
	)

	// parse arg
	if len(args.ID) > 0 {
		identity, err = mod.ResolveIdentity(args.ID)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	res := mod.ApplyFilters(identity, filters...)

	return ch.Send((*astral.Bool)(&res))
}
