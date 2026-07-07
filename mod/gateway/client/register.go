package gateway

import (
	gw "github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Register announces this node to the gateway with the given visibility and returns the Socket that represents the registration.
// The query channel is closed before returning; the Socket is independent of the channel lifetime.
func (c *Client) Register(ctx *astral.Context, visibility gw.Visibility) (*gw.Socket, error) {
	ch, err := c.queryCh(ctx, gw.MethodNodeRegister, query.Args{"visibility": string(visibility)})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	var socket *gw.Socket
	err = ch.Switch(
		channel.Expect(&socket),
		channel.PassErrors,
	)

	return socket, err
}
