package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Scan streams the repo's object IDs over the returned channel; the error pointer is valid only after it closes.
// With follow, a nil ID separates the initial snapshot from subsequent live updates.
func (client *Client) Scan(ctx *astral.Context, repo string, follow bool) (<-chan *astral.ObjectID, *error) {
	ch, err := client.queryCh(ctx, objects.MethodScan, query.Args{
		"repo":   repo,
		"follow": follow,
	})
	if err != nil {
		return nil, &err
	}

	out := make(chan *astral.ObjectID)

	var errPtr = new(error)

	go func() {
		defer close(out)
		defer ch.Close()

		*errPtr = ch.Switch(channel.Chan(out), channel.BreakOnEOS, channel.PassErrors, channel.WithContext(ctx))
		if *errPtr != nil {
			return
		}

		if !follow {
			return
		}

		// send the separator
		select {
		case <-ctx.Done():
			return
		case out <- nil:
		}

		// handle updates
		*errPtr = ch.Switch(channel.Chan(out), channel.BreakOnEOS, channel.PassErrors, channel.WithContext(ctx))
	}()

	return out, errPtr
}

func Scan(ctx *astral.Context, repo string, follow bool) (<-chan *astral.ObjectID, *error) {
	return Default().Scan(ctx, repo, follow)
}
