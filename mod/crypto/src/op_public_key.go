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

	var sawEOS bool
	err = ch.Switch(
		func(key *crypto.PrivateKey) error {
			return ch.Send(secp256k1.PublicKey(key))
		},
		// why: a composed upstream op reports a failed item as a wrong-typed
		// object in the stream; reply in-band and keep the batch alive.
		func(obj astral.Object) error {
			return ch.Send(astral.Err(astral.NewErrUnexpectedObject(obj)))
		},
		channel.MarkEOS(&sawEOS),
	)
	// why: no EOS reply after EOF — the caller is gone and Conn closes on read error.
	if err != nil || !sawEOS {
		return err
	}
	return ch.Send(&astral.EOS{})
}
