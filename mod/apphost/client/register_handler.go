package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// RegisterHandler registers a new handler for incoming queries.
func (client *Client) RegisterHandler(ctx *astral.Context, endpoint string, authToken astral.Nonce) error {
	ch, err := client.queryCh(ctx, apphost.MethodRegisterHandler, query.Args{
		"endpoint": endpoint,
		"token":    authToken,
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(channel.ExpectAck, channel.PassErrors)
}

func RegisterHandler(ctx *astral.Context, endpoint string, authToken astral.Nonce) error {
	return Default().RegisterHandler(ctx, endpoint, authToken)
}
