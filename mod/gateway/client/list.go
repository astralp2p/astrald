package gateway

import (
	gw "github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (c *Client) List(ctx *astral.Context) ([]*astral.Identity, error) {
	ch, err := c.queryCh(ctx, gw.MethodNodeList, query.Args{})
	if err != nil {
		return nil, err
	}
	defer ch.Close()

	var list []*astral.Identity
	err = ch.Switch(
		channel.Collect(&list),
		channel.BreakOnEOS,
		channel.PassErrors,
	)

	return list, err
}
