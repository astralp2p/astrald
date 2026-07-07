package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type opPublicKeyArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpPublicKey(ctx *astral.Context, q *routing.IncomingQuery, args opPublicKeyArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = ch.Switch(
		func(key *crypto.PrivateKey) error {
			return ch.Send(secp256k1.PublicKey(key))
		},
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
