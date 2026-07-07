package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

type opSignTextArgs struct {
	Text   string `query:"optional"`
	Key    string `query:"optional"`
	Scheme string `query:"optional"`
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

func (mod *Module) OpSignText(ctx *astral.Context, q *routing.IncomingQuery, args opSignTextArgs) (err error) {
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

	signer, err := mod.NewTextSigner(signerKey, args.Scheme)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// signAndSend signs the text and sends the signature to the channel
	var signAndSend = func(text string) error {
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
		channel.BreakOnEOS,
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
