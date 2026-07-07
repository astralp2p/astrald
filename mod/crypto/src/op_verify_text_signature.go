package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/secp256k1"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opVerifyTextSignatureArgs struct {
	Text string `query:"optional"`
	Key  string `query:"optional"`
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

// OpVerifyTextSignature accepts a raw channel and verifies a text signature.
// Public key and text may come from query args or be streamed as channel
// messages; a Signature message triggers verification and ends the exchange.
// Defaults the public key to the caller's identity when none is supplied.
func (mod *Module) OpVerifyTextSignature(ctx *astral.Context, q *routing.IncomingQuery, args opVerifyTextSignatureArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var publicKey = secp256k1.FromIdentity(q.Caller())
	var text = args.Text

	// check key argument
	if len(args.Key) > 0 {
		err = publicKey.UnmarshalText([]byte(args.Key))
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	err = ch.Switch(
		func(key *crypto.PublicKey) error {
			publicKey = key
			return ch.Send(&astral.Ack{})
		},
		func(text2 *astral.String8) error {
			text = (string)(*text2)
			return ch.Send(&astral.Ack{})
		},
		func(text2 *astral.String16) error {
			text = (string)(*text2)
			return ch.Send(&astral.Ack{})
		},
		func(sig *crypto.Signature) error {
			switch {
			case publicKey == nil:
				return ch.Send(astral.NewError("missing public key"))
			case text == "":
				return ch.Send(astral.NewError("missing text"))
			}

			err = mod.VerifyTextSignature(publicKey, sig, text)
			if err != nil {
				return ch.Send(astral.Err(err))
			}
			return ch.Send(&astral.Ack{})
		},
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
