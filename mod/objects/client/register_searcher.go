package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// RegisterSearcher registers the caller as a searcher provider and blocks until acked.
func (client *Client) RegisterSearcher(ctx *astral.Context) error {
	ch, err := client.queryCh(ctx, objects.MethodRegisterSearcher, nil)
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.WithContext(ctx))
}

func RegisterSearcher(ctx *astral.Context) error {
	return Default().RegisterSearcher(ctx)
}
