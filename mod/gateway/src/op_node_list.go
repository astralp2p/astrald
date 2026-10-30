package gateway

import (
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListArgs struct {
	Out string
}

// OpNodeList streams the identities of all publicly visible registered nodes, terminated by EOS.
//
// why: no action: the op lists only nodes that registered with public visibility,
// which is their request to be listed. It changes no state.
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
