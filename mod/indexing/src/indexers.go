package indexing

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/indexing"
)

// indexerHandle is the in-memory view of a registered indexer.
// node is the tree node at /mod/indexing/indexers/<name> — its value is the
// nonce, and its sub-nodes are per-repo cursor versions.
type indexerHandle struct {
	name  string
	nonce astral.Nonce
	node  tree.Node
}

// state returns the cursor version this indexerHandle has acked in repoName.
// Zero means no sub-node exists yet.
func (i *indexerHandle) state(ctx *astral.Context, repoName string) (uint64, error) {
	subs, err := i.node.Sub(ctx)
	if err != nil {
		return 0, err
	}

	sub, ok := subs[repoName]
	if !ok {
		return 0, nil
	}

	v, err := tree.Get[*astral.Uint64](ctx, sub)
	if err != nil {
		return 0, err
	}

	return uint64(*v), nil
}

func (i *indexerHandle) setState(ctx *astral.Context, repoName string, version uint64) error {
	current, err := i.state(ctx, repoName)
	if err != nil {
		return err
	}
	if version != current+1 {
		return indexing.ErrInvalidIndexHeight
	}

	sub, err := tree.Query(ctx, i.node, repoName, true)
	if err != nil {
		return err
	}

	v := astral.Uint64(version)
	return sub.Set(ctx, &v)
}

// RegisterIndexer creates a named indexerHandle if it does not exist yet and returns
// its stable nonce. Height 0 is represented by the absence of per-repo cursor
// nodes under the indexerHandle.
func (mod *Module) RegisterIndexer(ctx *astral.Context, name string) (astral.Nonce, error) {
	existing, err := mod.findIndexerByName(ctx, name)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.nonce, nil
	}

	node, err := mod.indexers.Create(ctx, name)
	if err != nil {
		// Concurrent register lost the race; return the winner's nonce.
		if errors.Is(err, tree.ErrAlreadyExists) {
			existing, err = mod.findIndexerByName(ctx, name)
			if err != nil {
				return 0, err
			}
			if existing != nil {
				return existing.nonce, nil
			}
		}
		return 0, err
	}

	nonce := astral.NewNonce()
	if err := node.Set(ctx, &nonce); err != nil {
		return 0, err
	}

	return nonce, nil
}

// RemoveIndexer deletes the indexerHandle registration and all of its cursor
// sub-nodes. Returns ErrIndexNotFound if no indexerHandle matches the nonce.
func (mod *Module) RemoveIndexer(ctx *astral.Context, nonce astral.Nonce) error {
	idxer, err := mod.findIndexerByNonce(ctx, nonce)
	if err != nil {
		return err
	}
	if idxer == nil {
		return indexing.ErrIndexNotFound
	}

	return deleteIndexerTree(ctx, idxer.node)
}

// UpdateIndexerState advances the cursor version for repoName on the indexerHandle
// identified by nonce.
func (mod *Module) UpdateIndexerState(ctx *astral.Context, nonce astral.Nonce, repoName string, version uint64) error {
	idxer, err := mod.findIndexerByNonce(ctx, nonce)
	if err != nil {
		return err
	}
	if idxer == nil {
		return indexing.ErrIndexNotFound
	}

	return idxer.setState(ctx, repoName, version)
}

func (mod *Module) findIndexerByName(ctx *astral.Context, name string) (*indexerHandle, error) {
	subs, err := mod.indexers.Sub(ctx)
	if err != nil {
		return nil, err
	}

	node, ok := subs[name]
	if !ok {
		return nil, nil
	}

	nonce, err := tree.Get[*astral.Nonce](ctx, node)
	if err != nil {
		return nil, err
	}

	return &indexerHandle{name: name, nonce: *nonce, node: node}, nil
}

func (mod *Module) findIndexerByNonce(ctx *astral.Context, nonce astral.Nonce) (*indexerHandle, error) {
	subs, err := mod.indexers.Sub(ctx)
	if err != nil {
		return nil, err
	}

	for name, node := range subs {
		storedNonce, err := tree.Get[*astral.Nonce](ctx, node)
		if err != nil {
			return nil, err
		}
		if *storedNonce == nonce {
			return &indexerHandle{name: name, nonce: *storedNonce, node: node}, nil
		}
	}

	return nil, nil
}

func deleteIndexerTree(ctx *astral.Context, node tree.Node) error {
	subs, err := node.Sub(ctx)
	if err != nil {
		return err
	}

	for _, sub := range subs {
		if err := deleteIndexerTree(ctx, sub); err != nil {
			return err
		}
	}

	return node.Delete(ctx)
}

// pickNextChange scans enabled repos and returns the first one with an unacked change.
func (mod *Module) pickNextChange(ctx *astral.Context, idxer *indexerHandle) (string, *dbRepoEntry, error) {
	for _, repoName := range mod.enabledRepos() {
		version, err := idxer.state(ctx, repoName)
		if err != nil {
			return "", nil, err
		}

		change, err := mod.db.nextChange(repoName, version)
		if err != nil {
			return "", nil, err
		}

		if change != nil {
			return repoName, change, nil
		}
	}
	return "", nil, nil
}
