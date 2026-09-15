package crypto

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

type opPublicKeyArgs struct {
	In  string
	Out string
}

func (mod *Module) OpPublicKey(ctx *astral.Context, q *routing.IncomingQuery, args opPublicKeyArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var sawEOS bool
	err = ch.Switch(
		func(key *crypto.PrivateKey) error {
			publicKey := secp256k1.PublicKey(key)
			// why: secp256k1.PublicKey answers a key of another type with a nil
			// sentinel, and sending that nil calls a value method on a nil pointer,
			// which panics the op and closes the connection with no diagnostic.
			if publicKey == nil {
				return ch.Send(astral.Err(fmt.Errorf("%w: %v", cryptomod.ErrUnsupportedKeyType, key.Type)))
			}
			return ch.Send(publicKey)
		},
		// why: a composed upstream op reports a failed item as a wrong-typed
		// object in the stream; reply in-band and keep the batch alive.
		func(obj astral.Object) error {
			return ch.Send(astral.Err(astral.NewErrUnexpectedObject(obj)))
		},
		channel.MarkEOS(&sawEOS),
	)
	// why: a bare return closed the channel with nothing on it, so the peer could not
	// tell a rejected payload from a dropped transport. A failed report is discarded:
	// the channel is already broken and the Switch error is the one worth returning.
	if err != nil {
		_ = ch.Send(astral.Err(err))
		return err
	}
	// why: no EOS reply after EOF — the caller is gone and Conn closes on read error.
	if !sawEOS {
		return nil
	}
	return ch.Send(&astral.EOS{})
}
