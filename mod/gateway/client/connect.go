package gateway

import (
	gw "github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Connect asks the gateway to open a connection to target and returns the resulting Socket.
// The query channel is closed before returning; the Socket is independent of the channel lifetime.
func (c *Client) Connect(ctx *astral.Context, target *astral.Identity) (*gw.Socket, error) {
	ch, err := c.queryCh(ctx, gw.MethodNodeConnect, query.Args{"target": target.String()})
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
