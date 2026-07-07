package tcp

import (
	"github.com/cryptopunkscc/astral-go/api/tcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) CloseEphemeralListener(ctx *astral.Context, port astral.Uint16) error {
	ch, err := client.queryCh(ctx, tcp.MethodCloseEphemeralListener, query.Args{
		"port": port,
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.Switch(
		channel.ExpectAck,
		func(msg *astral.ErrorMessage) error {
			return msg
		},
	)
}
