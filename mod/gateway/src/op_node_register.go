package gateway

import (
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNodeRegisterArgs struct {
	Visibility gateway.Visibility
	In         string `query:"optional"`
	Out        string `query:"optional"`
}

// OpNodeRegister registers the caller as a gateway-reachable node and returns the socket (nonce + TCP endpoint) the caller must listen on.
func (mod *Module) OpNodeRegister(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opNodeRegisterArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	socket, err := mod.register(ctx, q.Caller(), args.Visibility, "tcp")
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&socket)
}
