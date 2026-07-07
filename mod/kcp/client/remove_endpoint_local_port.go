package kcp

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/kcp"
)

func (client *Client) RemoveEndpointLocalPort(ctx *astral.Context, endpoint kcp.Endpoint) error {
	ch, err := client.queryCh(ctx, kcp.MethodRemoveEndpointLocalPort, query.Args{
		"endpoint": endpoint.Address(),
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(
		channel.ExpectAck,
		func(msg *astral.ErrorMessage) error {
			return msg
		},
	)
}
