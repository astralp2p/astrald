package indexing

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type Indexer interface {
	Add(id *astral.ObjectID, repo objects.Repository) error
	Remove(id *astral.ObjectID) error
}
