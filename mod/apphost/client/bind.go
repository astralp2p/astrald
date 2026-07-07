package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// Bind opens a persistent channel to the apphost; the caller must close it when done.
func (client *Client) Bind(ctx *astral.Context) (*channel.Channel, error) {
	ch, err := client.queryCh(ctx, apphost.MethodBind, nil)
	if err != nil {
		return nil, err
	}

	if err = ch.Switch(channel.ExpectAck, channel.PassErrors); err != nil {
		ch.Close()
		return nil, err
	}

	return ch, nil
}

func Bind(ctx *astral.Context) (*channel.Channel, error) {
	return Default().Bind(ctx)
}
