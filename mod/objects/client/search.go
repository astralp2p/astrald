package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Search streams results over the returned channel; the error pointer is valid only after it closes.
func (client *Client) Search(ctx *astral.Context, q objects.SearchQuery) (<-chan *objects.SearchResult, *error) {
	ch, err := client.queryCh(ctx, objects.MethodSearch, query.Args{
		"q": q,
	})
	if err != nil {
		return nil, &err
	}

	var out = make(chan *objects.SearchResult)
	var errPtr = new(error)

	go func() {
		defer close(out)
		defer ch.Close()

		*errPtr = ch.Switch(
			func(result *objects.SearchResult) error {
				return sig.Send(ctx, out, result)
			},
			channel.BreakOnEOS,
			channel.PassErrors,
			channel.WithContext(ctx),
		)
	}()

	return out, errPtr
}

func Search(ctx *astral.Context, q objects.SearchQuery) (<-chan *objects.SearchResult, *error) {
	return Default().Search(ctx, q)
}
