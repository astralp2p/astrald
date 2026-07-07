package indexing

import "github.com/cryptopunkscc/astral-go/astral"

const ModuleName = "indexing"
const DBPrefix = "indexing__"

// Module manages indexers that track object membership across named repositories.
// RegisterIndexer binds a named indexer and returns a nonce used to identify it in subsequent calls.
// UpdateIndexerState advances the acknowledged version for a repository, signalling sync progress.
type Module interface {
	RegisterIndexer(ctx *astral.Context, name string) (astral.Nonce, error)
	RemoveIndexer(ctx *astral.Context, nonce astral.Nonce) error
	UpdateIndexerState(ctx *astral.Context, nonce astral.Nonce, repoName string, version uint64) error
}
