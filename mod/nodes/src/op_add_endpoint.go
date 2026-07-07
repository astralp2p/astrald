package nodes

import (
	"errors"
	"strings"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opAddEndpointArgs struct {
	ID       *astral.Identity
	Endpoint string
	In       string `query:"optional"`
	Out      string `query:"optional"`
}

// OpAddEndpoint parses "network:address" and registers it for the identity with a
// fixed ~90-day TTL.
func (mod *Module) OpAddEndpoint(_ *astral.Context, q *routing.IncomingQuery, args opAddEndpointArgs) (err error) {
	chunks := strings.SplitN(args.Endpoint, ":", 2)
	if len(chunks) != 2 {
		return errors.New("invalid endpoint")
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	parse, err := mod.Exonet.Parse(chunks[0], chunks[1])
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.AddEndpoint(args.ID, nodes.NewEndpointWithTTL(parse, 3*30*24*time.Hour))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
