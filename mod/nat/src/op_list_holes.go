package nat

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListHolesArgs struct {
	With string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpListHoles(ctx *astral.Context, q *routing.IncomingQuery, args opListHolesArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	holes := mod.pool.GetAll()
	for _, hole := range holes {
		if args.With != "" {
			target, err := mod.Dir.ResolveIdentity(string(args.With))
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
