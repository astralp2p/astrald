package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterHandlerArgs struct {
	Endpoint string       `query:"required"`
	Token    astral.Nonce `query:"required"`
	In       string
	Out      string
}

func (mod *Module) OpRegisterHandler(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterHandlerArgs) (err error) {
	// cannot register handlers over a network
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	// why: read before accepting - the en-route entry naming this session is
	// dropped as soon as the query resolves.
	owner := mod.sessionOwner(q)

	if !mod.authorizeServeApps(ctx, q.Caller()) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// add the handler
	// note: Owner is the session that registered it, and is what apphost.bind
	// matches on. Identity is who the handler answers for.
	handler := &IPCHandler{
		Identity: q.Caller(),
		Owner:    owner,
		IPCToken: args.Token,
		Endpoint: args.Endpoint,
	}

	mod.ipcHandlers.Add(handler)

	mod.log.Logv(3, "%v registered a handler at %v", q.Caller(), args.Endpoint)

	return ch.Send(&astral.Ack{})
}
