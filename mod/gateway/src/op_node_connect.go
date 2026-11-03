package gateway

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNodeConnectArgs struct {
	Identity string `query:"required"`
	In       string
	Out      string
}

// OpNodeConnect handles the NodeConnect RPC: it reserves a pre-established idle
// connection to the named node and returns the nonce and endpoint the caller
// must use to claim it; the reservation expires after connectTimeout.
func (mod *Module) OpNodeConnect(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opNodeConnectArgs,
) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.authorizeUseGateway(ctx, q) {
		return ch.Send(auth.ErrNotPermitted)
	}

	target, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if target.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	socket, err := mod.reserveConn(q.Caller(), target, "tcp")
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&socket)
}
