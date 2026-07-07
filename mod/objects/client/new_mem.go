package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) NewMem(ctx *astral.Context, name string, size int64) error {
	// send the query
	ch, err := client.queryCh(ctx, objects.MethodNewMem, query.Args{
		"name": name,
		"size": size,
	})
	if err != nil {
		return err
	}
	defer ch.Close()

	// wait for ack
	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.WithContext(ctx))
}
