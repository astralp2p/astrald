package indexing

import (
	"github.com/cryptopunkscc/astral-go/api/indexing"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
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
