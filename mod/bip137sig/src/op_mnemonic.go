package src

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opMnemonicArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpMnemonic receives Entropy over the channel and replies with the space-joined mnemonic words.
func (mod *Module) OpMnemonic(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opMnemonicArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	object, err := ch.Receive()
	if err != nil {
		return err
	}

	entropy, ok := object.(*bip137sig.Entropy)
	if !ok {
		return ch.Send(astral.NewErrUnexpectedObject(object))
	}

	words, err := bip137sig.EntropyToMnemonic(*entropy)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(astral.NewString16(strings.Join(words, " ")))
}
