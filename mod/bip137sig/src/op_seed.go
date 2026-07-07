package src

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opSeedArgs struct {
	Passphrase string `query:"optional"`
	In         string `query:"optional"`
	Out        string `query:"optional"`
}

// OpSeed receives a whitespace-separated mnemonic and replies with the seed derived under Passphrase.
func (mod *Module) OpSeed(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opSeedArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	object, err := ch.Receive()
	if err != nil {
		return err
	}

	phrase, ok := object.(*astral.String16)
	if !ok {
		return ch.Send(astral.NewErrUnexpectedObject(object))
	}

	words := strings.Fields(string(*phrase))
	seed, err := bip137sig.MnemonicToSeed(words, args.Passphrase)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&seed)
}
