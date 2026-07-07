package gateway

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	gw "github.com/cryptopunkscc/astrald/mod/gateway"
)

func (c *Client) Unregister(ctx *astral.Context) error {
	ch, err := c.queryCh(ctx, gw.MethodNodeUnregister, query.Args{})
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(
		channel.ExpectAck,
		channel.PassErrors,
	)
}
