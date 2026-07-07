package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// RegisterFinder registers the caller as a finder provider and blocks until acked.
func (client *Client) RegisterFinder(ctx *astral.Context) error {
	ch, err := client.queryCh(ctx, objects.MethodRegisterFinder, nil)
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.WithContext(ctx))
}

func RegisterFinder(ctx *astral.Context) error {
	return Default().RegisterFinder(ctx)
}
