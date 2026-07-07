package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

type MigrateSessionArgs struct {
	SessionID astral.Nonce
	LinkID    astral.Nonce
}

func (client *Client) MigrateSession(ctx *astral.Context, args MigrateSessionArgs) (*channel.Channel, error) {
	return client.queryCh(ctx, nodes.MethodMigrateSession, args)
}
