package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/dir"
)

func SetAlias(ctx *astral.Context, identity *astral.Identity, alias string) error {
	return Default().SetAlias(ctx, identity, alias)
}

func (client *Client) SetAlias(ctx *astral.Context, identity *astral.Identity, alias string) error {
	ch, err := client.queryCh(ctx, dir.MethodSetAlias, query.Args{
		"id":    identity,
		"alias": alias,
	})
	if err != nil {
		return err
	}

	return ch.Switch(channel.ExpectAck, channel.PassErrors)
}
