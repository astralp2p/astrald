package kcp

import (
	"github.com/cryptopunkscc/astral-go/api/kcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// SetEndpointLocalPort maps endpoint to localPort; replace controls whether an existing mapping is overwritten.
func (client *Client) SetEndpointLocalPort(ctx *astral.Context, endpoint kcp.Endpoint, localPort astral.Uint16, replace bool) error {
	ch, err := client.queryCh(ctx, kcp.MethodSetEndpointLocalPort, query.Args{
		"endpoint":   endpoint.Address(),
		"local_port": localPort,
		"replace":    replace,
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
