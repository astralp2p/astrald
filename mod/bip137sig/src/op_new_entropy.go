package src

import (
	"github.com/cryptopunkscc/astral-go/api/bip137sig"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opNewEntropyArgs struct {
	Bits int    `query:"optional"`
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

// OpNewEntropy replies with fresh entropy of Bits length, defaulting to DefaultEntropyBits.
func (mod *Module) OpNewEntropy(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opNewEntropyArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	bits := args.Bits
	if bits == 0 {
		bits = bip137sig.DefaultEntropyBits
	}

	entropy, err := bip137sig.NewEntropy(bits)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&entropy)
}
