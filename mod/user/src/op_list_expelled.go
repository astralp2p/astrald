package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opListExpelledArgs struct {
	Out string `query:"optional"`
}

// OpListExpelled streams the signed bans issued by the active swarm user, terminated by EOS.
// Readable by any caller, matching OpListSiblings / OpSwarmStatus.
func (mod *Module) OpListExpelled(ctx *astral.Context, q *routing.IncomingQuery, args opListExpelledArgs) (err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return q.RejectWithCode(2)
	}

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	expulsions, err := mod.db.Expulsions(ac.Issuer)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	for _, se := range expulsions {
		err = ch.Send(se)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
