package objects

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// Read returns a stream of the object's bytes from offset; caller must Close it.
func (client *Client) Read(ctx *astral.Context, objectID *astral.ObjectID, offset, limit int64) (io.ReadCloser, error) {
	return client.query(ctx, objects.MethodRead, query.Args{
		"id":     objectID,
		"offset": offset,
		"limit":  limit,
		"zone":   "dvn",
	})
}

func Read(ctx *astral.Context, objectID *astral.ObjectID, offset, limit int64) (io.ReadCloser, error) {
	return Default().Read(ctx, objectID, offset, limit)
}
