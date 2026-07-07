package indexing

import (
	"fmt"

	"github.com/cryptopunkscc/astral-go/api/indexing"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (c *Client) RegisterIndexer(ctx *astral.Context, name string) (astral.Nonce, error) {
	ch, err := c.queryCh(ctx, indexing.MethodRegisterIndexer, query.Args{
		"name": name,
	})
	if err != nil {
		return 0, err
	}
	defer ch.Close()

	var indexerNonce *astral.Nonce
	err = ch.Switch(channel.Expect(&indexerNonce), channel.PassErrors)
	if err != nil {
		return 0, err
	}

	if indexerNonce == nil {
		return 0, fmt.Errorf(`indexer nonce is nil`)
	}

	return *indexerNonce, nil
}

func RegisterIndexer(ctx *astral.Context, name string) (astral.Nonce, error) {
	return Default().RegisterIndexer(ctx, name)
}
