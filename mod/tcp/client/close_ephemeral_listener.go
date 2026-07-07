package tcp

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/tcp"
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
