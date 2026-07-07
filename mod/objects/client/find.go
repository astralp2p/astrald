package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Find streams identities holding the object until EOS, then closes the channel.
// Zero identities are skipped. The error pointer is only valid once the channel is closed.
func (client *Client) Find(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *astral.Identity, *error) {
	ch, err := client.queryCh(ctx, objects.MethodFind, query.Args{
		"id": objectID,
	})
	if err != nil {
		return nil, &err
	}

	out := make(chan *astral.Identity)
	errPtr := new(error)

	go func() {
		defer close(out)
		defer ch.Close()

		*errPtr = ch.Switch(
			func(id *astral.Identity) error {
				if id != nil && !id.IsZero() {
					return sig.Send(ctx, out, id)
				}
				return nil
			},
			channel.BreakOnEOS,
			channel.PassErrors,
			channel.WithContext(ctx),
		)
	}()

	return out, errPtr
}

func Find(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *astral.Identity, *error) {
	return Default().Find(ctx, objectID)
}
