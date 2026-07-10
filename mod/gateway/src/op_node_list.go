package gateway

import (
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListArgs struct {
	Out string `query:"optional"`
}

// OpNodeList streams the identities of all publicly visible registered nodes, terminated by EOS.
func (mod *Module) OpNodeList(ctx *astral.Context, q *routing.IncomingQuery, args opListArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, client := range mod.registeredNodes.Values() {
		if client.GetVisibility() != gateway.VisibilityPublic {
			continue
		}
		if err := ch.Send(client.Identity); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
