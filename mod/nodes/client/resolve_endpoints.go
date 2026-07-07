package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

func (client *Client) ResolveEndpoints(ctx *astral.Context, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	ch, err := client.queryCh(ctx, nodes.MethodResolveEndpoints, query.Args{
		"id": identity,
	})
	if err != nil {
		return nil, err
	}

	var endpoints []*nodes.EndpointWithTTL
	err = ch.Switch(
		channel.Collect(&endpoints),
		channel.BreakOnEOS,
		func(msg *astral.ErrorMessage) error {
			return msg
		},
	)

	return endpoints, err
}
