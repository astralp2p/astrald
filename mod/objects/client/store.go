package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

func (client *Client) Store(ctx *astral.Context, repo string, object astral.Object) (id *astral.ObjectID, err error) {
	ch, err := client.queryCh(ctx, objects.MethodStore, query.Args{"repo": repo})
	if err != nil {
		return
	}
	defer ch.Close()
	if err = ch.Send(object); err != nil {
		return
	}
	err = ch.Switch(channel.Expect(&id), channel.PassErrors)
	return
}
