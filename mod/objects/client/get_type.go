package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Deprecated: use Probe instead.
func (client *Client) GetType(ctx *astral.Context, objectID *astral.ObjectID) (typ string, err error) {
	ch, err := client.queryCh(ctx, objects.MethodGetType, query.Args{
		"id": objectID,
	})
	if err != nil {
		return "", err
	}
	defer ch.Close()

	err = ch.Switch(
		channel.ExpectString[*astral.String8](&typ),
		channel.PassErrors,
		channel.WithContext(ctx),
	)

	return
}
