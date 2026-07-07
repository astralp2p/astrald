package gateway

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opNodeConnectArgs struct {
	Target *astral.Identity
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

// OpNodeConnect handles the NodeConnect RPC: it reserves a pre-established idle
// connection to the target node and returns the nonce and endpoint the caller
// must use to claim it; the reservation expires after connectTimeout.
func (mod *Module) OpNodeConnect(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opNodeConnectArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	socket, err := mod.reserveConn(q.Caller(), args.Target, "tcp")
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&socket)
}
