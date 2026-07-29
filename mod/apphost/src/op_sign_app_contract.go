package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSignAppContractArgs struct {
	In  string
	Out string
}

func (mod *Module) OpSignAppContract(ctx *astral.Context, q *routing.IncomingQuery, args opSignAppContractArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return channel.Batch(ch, func(c *auth.Contract) astral.Object {
		signed := &auth.SignedContract{Contract: c}
		if err := mod.Auth.SignContract(ctx, signed); err != nil {
			return astral.Err(err)
		}

		if err := mod.Auth.IndexContract(ctx, signed); err != nil {
			return astral.Err(err)
		}

		if _, err := mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
			return astral.Err(err)
		}

		go mod.User.PushToLocalSwarm(mod.ctx, signed)

		mod.log.Logv(1, "signed app contract (%v)", signed.Issuer)
		return signed
	})
}
