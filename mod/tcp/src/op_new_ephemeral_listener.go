package tcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNewEphemeralListenerArgs struct {
	Port astral.Uint16
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

// OpNewEphemeralListener handles a remote request to open an ephemeral TCP listener on the given port,
// using the module's own acceptAll handler to establish inbound links for every accepted connection.
func (mod *Module) OpNewEphemeralListener(ctx *astral.Context, q *routing.IncomingQuery, args opNewEphemeralListenerArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.CreateEphemeralListener(ctx, args.Port, mod.acceptAll)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
