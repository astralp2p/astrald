package crypto

import (
	"encoding/hex"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
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
	var sawEOS bool
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
