package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSignContractArgs struct {
	In  string
	Out string
}

// OpSignContract handles the sign-contract remote operation: reads a Contract from the
// channel, signs it as the local node, and writes the resulting SignedContract back.
func (mod *Module) OpSignContract(ctx *astral.Context, q *routing.IncomingQuery, args opSignContractArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(c *auth.Contract) astral.Object {
		signed := &auth.SignedContract{Contract: c}
		if err := mod.SignContract(ctx, signed); err != nil {
			return astral.Err(err)
		}
		return signed
	})
}
