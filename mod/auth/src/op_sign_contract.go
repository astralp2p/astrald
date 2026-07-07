package auth

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opSignContractArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpSignContract handles the sign-contract remote operation: reads a Contract from the
// channel, signs it as the local node, and writes the resulting SignedContract back.
func (mod *Module) OpSignContract(ctx *astral.Context, q *routing.IncomingQuery, args opSignContractArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err := ch.Switch(func(c *auth.Contract) error {
		signed := &auth.SignedContract{Contract: c}
		err := mod.SignContract(ctx, signed)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		return ch.Send(signed)
	},
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
