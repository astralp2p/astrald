package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// RegisterDescriber registers the caller as a describer provider and blocks until acked.
func (client *Client) RegisterDescriber(ctx *astral.Context) error {
	ch, err := client.queryCh(ctx, objects.MethodRegisterDescriber, nil)
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.WithContext(ctx))
}

func RegisterDescriber(ctx *astral.Context) error {
	return Default().RegisterDescriber(ctx)
}
