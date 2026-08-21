package indexing

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

type Indexer interface {
	Add(id *astral.ObjectID, repo objects.Repository) error
	Remove(id *astral.ObjectID) error
}
