package dir

import (
	"strings"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opApplyFiltersArgs struct {
	Filters  string `query:"required"`
	Identity string
	In       string
	Out      string
}

func (mod *Module) OpApplyFilters(ctx *astral.Context, q *routing.IncomingQuery, args opApplyFiltersArgs) (err error) {
	if !mod.authorizeSeeNodeState(ctx, q) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// set initial values
	var (
		identity = q.Caller()
		filters  = strings.Split(args.Filters, ",")
	)

	// parse arg
	if len(args.Identity) > 0 {
		identity, err = mod.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	res := mod.ApplyFilters(identity, filters...)

	return ch.Send((*astral.Bool)(&res))
}
