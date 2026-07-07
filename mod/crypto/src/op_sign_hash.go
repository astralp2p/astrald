package crypto

import (
	"encoding/hex"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type opSignHashArgs struct {
	Hash   string `query:"optional"`
	Key    string `query:"optional"`
	Scheme string `query:"optional"`
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

func (mod *Module) OpSignHash(ctx *astral.Context, q *routing.IncomingQuery, args opSignHashArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// set defaults
	if args.Scheme == "" {
		args.Scheme = "asn1"
	}

	var signerKey = secp256k1.FromIdentity(q.Caller()) // check key argument

	// parse key argument
	if len(args.Key) > 0 {
		signerKey = &crypto.PublicKey{}
		err = signerKey.UnmarshalText([]byte(args.Key))
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	var signAndSend = func(hash []byte) error {
		signer, err := mod.NewHashSigner(signerKey, args.Scheme)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}

		sig, err := signer.SignHash(ctx, hash)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}

		return ch.Send(sig)
	}

	if len(args.Hash) > 0 {
		hash, err := hex.DecodeString(args.Hash)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}

		return signAndSend(hash)
	}

	err = ch.Switch(
		func(key *crypto.PublicKey) error {
			signerKey = key
			return ch.Send(&astral.Ack{})
		},
		func(hash *crypto.Hash) error {
			return signAndSend(*hash)
		},
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
