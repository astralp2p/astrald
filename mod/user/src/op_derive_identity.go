package user

import (
	"strings"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDeriveIdentityArgs struct {
	Generate   bool
	Passphrase string
	In         string
	Out        string
}

// OpDeriveIdentity answers the user that BIP-39 words derive along
// UserKeyPath, the user OpBootstrap claims the node for with the same words,
// and changes no state. The words arrive in the stream, or the node generates
// them under Generate and returns them in the reply.
// Spec: .ai/system/protocols/user/ops/user.derive_identity.md
func (mod *Module) OpDeriveIdentity(ctx *astral.Context, q *routing.IncomingQuery, args opDeriveIdentityArgs) error {
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	words, err := receiveWords(ch, args.Generate)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	key, err := mod.deriveUserKey(words, args.Passphrase)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	result := &user.DeriveIdentityResult{Identity: secp256k1.Identity(secp256k1.PublicKey(&key))}
	if args.Generate {
		result.Mnemonic = astral.String16(strings.Join(words, " "))
	}

	return ch.Send(result)
}
