package kcp

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opNewEphemeralListenerArgs struct {
	Port astral.Uint16
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpNewEphemeralListener(ctx *astral.Context, q *routing.IncomingQuery, args opNewEphemeralListenerArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.CreateEphemeralListener(args.Port)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
