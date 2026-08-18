package objects

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

func (mod *Module) AddIndexer(indexer objectsmod.Indexer) error {
	if indexer == nil {
		return errors.New("indexer is nil")
	}

	return mod.indexers.Add(indexer)
}

// index offers a freshly stored object to every indexer, synchronously.
//
// why: the repository follower picks the object up on its own schedule, so a
// caller that stores a private key and signs with it in the next call can beat
// its own index and get "sign as issuer: unsupported". Indexing here makes the
// store reply mean what callers already assume it means.
//
// note: an indexer that does not answer for this object type returns
// ErrUnexpectedObject — the common case, and not a failure. Indexing is
// advisory: the follower still indexes, and a failing indexer must not void a
// store that already committed.
func (mod *Module) index(object astral.Object) {
	for _, indexer := range mod.indexers.Clone() {
		err := indexer.AddToIndex(object)
		if err == nil || errors.Is(err, &astral.ErrUnexpectedObject{}) {
			continue
		}

		mod.log.Errorv(1, "indexer %T on %v: %v", indexer, object.ObjectType(), err)
	}
}
