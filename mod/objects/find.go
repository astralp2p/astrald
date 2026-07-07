package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

// Finder is used to figure out which identities can provide access to an object
type Finder interface {
	FindObject(*astral.Context, *astral.ObjectID) (<-chan *astral.Identity, error)
}
