package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Purge streams the IDs of purged objects until EOS, then closes the channel.
// The error pointer is only valid once the channel is closed.
func (client *Client) Purge(ctx *astral.Context, repo string) (<-chan *astral.ObjectID, *error) {
	ch, err := client.queryCh(ctx, objects.MethodPurge, query.Args{
		"repo": repo,
	})
	if err != nil {
		return nil, &err
	}

	out := make(chan *astral.ObjectID)
	errPtr := new(error)

	go func() {
		defer close(out)
		defer ch.Close()

		*errPtr = ch.Switch(
			func(id *astral.ObjectID) error {
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

func Purge(ctx *astral.Context, repo string) (<-chan *astral.ObjectID, *error) {
	return Default().Purge(ctx, repo)
}
