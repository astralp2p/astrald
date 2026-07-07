package crypto

import (
	"encoding/hex"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type opVerifyHashSignatureArgs struct {
	Hash string `query:"optional"`
	Key  string `query:"optional"`
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpVerifyHashSignature(ctx *astral.Context, q *routing.IncomingQuery, args opVerifyHashSignatureArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var publicKey *crypto.PublicKey
	var hash []byte

	// check hash argument
	if len(args.Hash) > 0 {
		hash, err = hex.DecodeString(args.Hash)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	// check key argument
	if len(args.Key) > 0 {
		publicKey = &crypto.PublicKey{}
		err = publicKey.UnmarshalText([]byte(args.Key))
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	} else {
		publicKey = secp256k1.FromIdentity(q.Caller())
	}

	// process the channel
	err = ch.Switch(
		func(sig *crypto.Signature) error {
			// check errors
			switch {
			case publicKey == nil:
				return ch.Send(astral.NewError("missing public key"))
			case hash == nil:
				return ch.Send(astral.NewError("missing hash"))
			}

			// verify signature
			err = mod.VerifyHashSignature(publicKey, sig, hash)
			if err != nil {
				return ch.Send(astral.Err(err))
			}
			return ch.Send(&astral.Ack{})
		},
		func(key *crypto.PublicKey) error {
			publicKey = key
			return ch.Send(&astral.Ack{})
		},
		func(hash2 *crypto.Hash) error {
			hash = *hash2
			return ch.Send(&astral.Ack{})
		},
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
