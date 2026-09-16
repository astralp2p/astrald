package nodes

import (
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAddEndpointArgs struct {
	Identity string `query:"required"`
	Endpoint string `query:"required"`
	In       string
	Out      string
}

// OpAddEndpoint parses "network:address" and registers it for the identity with a
// fixed ~90-day TTL.
func (mod *Module) OpAddEndpoint(ctx *astral.Context, q *routing.IncomingQuery, args opAddEndpointArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if identity.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	chunks := strings.SplitN(args.Endpoint, ":", 2)
	if len(chunks) != 2 {
		return ch.Send(astral.NewError("invalid endpoint"))
	}

	parse, err := mod.Exonet.Parse(chunks[0], chunks[1])
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.AddEndpoint(identity, nodes.NewEndpointWithTTL(parse, 3*30*24*time.Hour))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
