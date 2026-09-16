package nat

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListHolesArgs struct {
	Identity string
	Out      string
}

func (mod *Module) OpListHoles(ctx *astral.Context, q *routing.IncomingQuery, args opListHolesArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	var target *astral.Identity
	if args.Identity != "" {
		target, err = mod.Dir.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	holes := mod.pool.GetAll()
	for _, hole := range holes {
		if target != nil && !hole.MatchesPeer(target) {
			continue
		}

		err = ch.Send(&hole.Hole)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
