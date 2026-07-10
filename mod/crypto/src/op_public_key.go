package crypto

import (
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
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
