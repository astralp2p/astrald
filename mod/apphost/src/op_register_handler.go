package apphost

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opRegisterHandlerArgs struct {
	Endpoint string
	Token    astral.Nonce
	In       string `query:"optional"`
	Out      string `query:"optional"`
}

func (mod *Module) OpRegisterHandler(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterHandlerArgs) (err error) {
	// cannot register handlers over a network
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// add the handler
	handler := &IPCHandler{
		Identity: q.Caller(),
		IPCToken: args.Token,
		Endpoint: args.Endpoint,
	}

	mod.ipcHandlers.Add(handler)

	mod.log.Logv(3, "%v registered a handler at %v", q.Caller(), args.Endpoint)

	return ch.Send(&astral.Ack{})
}
