package user

import (
	"github.com/astralp2p/astral-go/astral"
)

// FindObject streams the identities of all currently-linked sibling nodes as candidate holders for any object.
// The object ID is intentionally ignored — every sibling may hold the object.
func (mod *Module) FindObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *astral.Identity, error) {
	out := make(chan *astral.Identity)
	go func() {
		defer close(out)

		for _, sib := range mod.getSiblings() {
			select {
			case out <- sib:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}
