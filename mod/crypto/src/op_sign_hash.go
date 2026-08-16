package crypto

import (
	"encoding/hex"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSignHashArgs struct {
	Hash   string
	Key    string
	Scheme string
	In     string
	Out    string
}

// OpSignHash signs a hash under the caller's own key. Local callers only; a
// peer signs on its own node.
func (mod *Module) OpSignHash(ctx *astral.Context, q *routing.IncomingQuery, args opSignHashArgs) (err error) {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

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
		// why: the authorization sits here rather than beside the Key argument
		// because the channel loop below rebinds signerKey mid-stream, so a
		// check at parse time is bypassed by the second frame. Every signature
		// passes through this function.
		if err := mod.authorizeSigner(q.Caller(), signerKey); err != nil {
			return ch.Send(astral.Err(err))
		}

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

	var sawEOS bool
	err = ch.Switch(
		func(key *crypto.PublicKey) error {
			signerKey = key
			return ch.Send(&astral.Ack{})
		},
		func(hash *crypto.Hash) error {
			return signAndSend(*hash)
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
