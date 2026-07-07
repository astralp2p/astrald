package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

// FindObject returns providers for an object from the local search cache only; it
// performs no live network lookup. The channel is already closed on return.
func (mod *Module) FindObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *astral.Identity, error) {
	out := make(chan *astral.Identity, 1)
	defer close(out)

	if id, found := mod.searchCache.Get(id.String()); found {
		out <- id
	}

	return out, nil
}
