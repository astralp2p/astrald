package indexing

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/indexing"
)

func (c *Client) RemoveIndex(ctx *astral.Context, nonce astral.Nonce) error {
	ch, err := c.queryCh(ctx, indexing.MethodRemoveIndex, query.Args{
		"nonce": nonce,
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	var ack *astral.Ack
	return ch.Switch(channel.Expect(&ack), channel.PassErrors)
}

func RemoveIndex(ctx *astral.Context, nonce astral.Nonce) error {
	return Default().RemoveIndex(ctx, nonce)
}
