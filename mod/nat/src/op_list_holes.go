package nat

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListHolesArgs struct {
	Identity string
	Out      string
}

func (mod *Module) OpListHoles(ctx *astral.Context, q *routing.IncomingQuery, args opListHolesArgs) (err error) {
	if !mod.authorizeAdminNetwork(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	holes := mod.pool.GetAll()
	for _, hole := range holes {
		if args.Identity != "" {
			target, err := mod.Dir.ResolveIdentity(args.Identity)
			if err != nil {
				return ch.Send(astral.NewError(err.Error()))
			}

			if !hole.MatchesPeer(target) {
				continue
			}
		}

		err = ch.Send(&hole.Hole)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
