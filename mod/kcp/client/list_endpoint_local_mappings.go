package kcp

import (
	"github.com/cryptopunkscc/astral-go/api/kcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

func (client *Client) ListEndpointLocalMappings(ctx *astral.Context) ([]*kcp.EndpointLocalMapping, error) {
	ch, err := client.queryCh(ctx, kcp.MethodListEndpointLocalMappings, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	var mappings []*kcp.EndpointLocalMapping

	err = ch.Switch(
		channel.Collect(&mappings),
		channel.BreakOnEOS,
		func(msg *astral.ErrorMessage) error {
			return msg
		},
	)

	return mappings, err
}
