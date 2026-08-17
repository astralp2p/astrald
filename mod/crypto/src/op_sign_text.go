package crypto

import (
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSignTextArgs struct {
	Text   string
	Key    string
	Scheme string
	In     string
	Out    string
}

// OpSignText signs a message under the caller's own key. Local callers only; a
// peer signs on its own node.
func (mod *Module) OpSignText(ctx *astral.Context, q *routing.IncomingQuery, args opSignTextArgs) (err error) {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// set defaults
	if args.Scheme == "" {
		args.Scheme = "bip137"
	}

	var signerKey = secp256k1.FromIdentity(q.Caller())

	// check key argument
	if len(args.Key) > 0 {
		err = signerKey.UnmarshalText([]byte(args.Key))
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	// signAndSend signs the text and sends the signature to the channel
	var signAndSend = func(text string) error {
		// why: the authorization sits here rather than beside the Key argument
		// because the channel loop below rebinds signerKey mid-stream, so a
		// check at parse time is bypassed by the second frame. Every signature
		// passes through this function.
		if err := mod.authorizeSigner(ctx, q.Caller(), signerKey); err != nil {
			return ch.Send(astral.Err(err))
		}

		// why: the signer is built per signature rather than once before the
		// loop, so the key the loop acknowledges is the key that signs. Built
		// once, a mid-stream key was acked and then ignored.
		signer, err := mod.NewTextSigner(signerKey, args.Scheme)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		sig, err := signer.SignText(ctx, text)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
		return ch.Send(sig)
	}

	if len(args.Text) > 0 {
		return signAndSend(args.Text)
	}

	// process channel
	var sawEOS bool
	err = ch.Switch(
		func(key *crypto.PublicKey) error {
			signerKey = key
			return ch.Send(&astral.Ack{})
		},
		func(text *astral.String8) error {
			return signAndSend(text.String())
		},
		func(text *astral.String16) error {
			return signAndSend(text.String())
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
