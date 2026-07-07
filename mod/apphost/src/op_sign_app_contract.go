package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opSignAppContractArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpSignAppContract(ctx *astral.Context, q *routing.IncomingQuery, args opSignAppContractArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err := ch.Switch(func(c *auth.Contract) error {
		signed := &auth.SignedContract{Contract: c}
		err := mod.Auth.SignContract(ctx, signed)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		err = mod.Auth.IndexContract(ctx, signed)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		go mod.User.PushToLocalSwarm(mod.ctx, signed)

		mod.log.Logv(1, "signed app contract (%v)", signed.Issuer)
		return ch.Send(signed)
	})
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
