package indexing

import (
	"errors"
	indexingmod "github.com/astralp2p/astrald/mod/indexing"

	"github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// Sub-nodes of a registration at /mod/indexing/indexers/<name>.
const (
	ownerNode   = "owner"
	cursorsNode = "cursors"
)

// indexerHandle is the in-memory view of a registered indexer.
// node is the tree node at /mod/indexing/indexers/<name> — its value is the
// nonce, its sub-node owner holds the registering identity, and its sub-node
// cursors holds per-repo cursor versions.
type indexerHandle struct {
	name  string
	nonce astral.Nonce
	owner *astral.Identity
	node  tree.Node
}

// ownedBy reports whether id registered this indexer.
// note: a registration without an owner is owned by nobody.
func (i *indexerHandle) ownedBy(id *astral.Identity) bool {
	return !i.owner.IsZero() && i.owner.IsEqual(id)
}

// nonceFor returns the registration's nonce to its owner.
func (i *indexerHandle) nonceFor(id *astral.Identity) (astral.Nonce, error) {
	if !i.ownedBy(id) {
		return 0, indexing.ErrIndexerNameTaken
	}
	return i.nonce, nil
}

// state returns the cursor version this indexerHandle has acked in repoName.
// Zero means no cursor node exists yet.
func (i *indexerHandle) state(ctx *astral.Context, repoName string) (uint64, error) {
	subs, err := i.node.Sub(ctx)
	if err != nil {
		return 0, err
	}

	cursors, ok := subs[cursorsNode]
	if !ok {
		return 0, nil
	}

	subs, err = cursors.Sub(ctx)
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
		return indexingmod.ErrInvalidIndexHeight
	}

	sub, err := tree.Query(ctx, i.node, cursorsNode+"/"+repoName, true)
	if err != nil {
		return err
	}

	v := astral.Uint64(version)
	return sub.Set(ctx, &v)
}

// RegisterIndexer creates a named indexerHandle owned by owner if the name is
// free and returns its stable nonce. Height 0 is represented by the absence of
// cursor nodes under the indexerHandle. A name another identity registered
// answers ErrIndexerNameTaken.
func (mod *Module) RegisterIndexer(ctx *astral.Context, owner *astral.Identity, name string) (astral.Nonce, error) {
	existing, err := mod.findIndexerByName(ctx, name)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.nonceFor(owner)
	}

	node, err := mod.indexers.Create(ctx, name)
	if errors.Is(err, tree.ErrAlreadyExists) {
		// note: a concurrent register won the race; the winner's owner decides.
		existing, err = mod.findIndexerByName(ctx, name)
		if err != nil {
			return 0, err
		}
		if existing == nil {
			return 0, tree.ErrAlreadyExists
		}
		return existing.nonceFor(owner)
	}
	if err != nil {
		return 0, err
	}

	return writeRegistration(ctx, node, owner)
}

// writeRegistration stores owner and a new nonce on a created registration node.
//
// why: the owner is written before the nonce, so a registration that answers
// with a nonce always names its owner.
func writeRegistration(ctx *astral.Context, node tree.Node, owner *astral.Identity) (astral.Nonce, error) {
	ownerSub, err := tree.Query(ctx, node, ownerNode, true)
	if err != nil {
		return 0, err
	}
	if err := ownerSub.Set(ctx, owner); err != nil {
		return 0, err
	}

	nonce := astral.NewNonce()
	if err := node.Set(ctx, &nonce); err != nil {
		return 0, err
	}

	return nonce, nil
}

// UnregisterIndexer deletes the indexerHandle registration and all of its cursor
// sub-nodes. Returns ErrIndexNotFound if no indexerHandle matches the nonce.
func (mod *Module) UnregisterIndexer(ctx *astral.Context, nonce astral.Nonce) error {
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

	return loadHandle(ctx, name, *nonce, node)
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
			return loadHandle(ctx, name, nonce, node)
		}
	}

	return nil, nil
}

func loadHandle(ctx *astral.Context, name string, nonce astral.Nonce, node tree.Node) (*indexerHandle, error) {
	owner, err := loadOwner(ctx, node)
	if err != nil {
		return nil, err
	}

	return &indexerHandle{name: name, nonce: nonce, owner: owner, node: node}, nil
}

// loadOwner returns the identity a registration node names as its owner, or nil.
//
// note: a registration stored before owners existed may hold a cursor for a
// repository named owner. A value that is not an identity names no owner.
func loadOwner(ctx *astral.Context, node tree.Node) (*astral.Identity, error) {
	subs, err := node.Sub(ctx)
	if err != nil {
		return nil, err
	}

	sub, ok := subs[ownerNode]
	if !ok {
		return nil, nil
	}

	value, err := sub.Get(ctx, false)
	if err != nil {
		return nil, err
	}

	owner, _ := (<-value).(*astral.Identity)
	return owner, nil
}

// deleteUnownedIndexers deletes every registration that names no owner, with its
// cursors, and returns how many it deleted.
//
// why: a registration stored before owners existed is owned by nobody, so no
// caller subscribes to it and its name is taken for every identity. Its indexer
// registers again and replays from version 0.
func (mod *Module) deleteUnownedIndexers(ctx *astral.Context) (int, error) {
	subs, err := mod.indexers.Sub(ctx)
	if err != nil {
		return 0, err
	}

	var deleted int
	for _, node := range subs {
		owner, err := loadOwner(ctx, node)
		if err != nil {
			return deleted, err
		}
		if !owner.IsZero() {
			continue
		}
		if err := deleteIndexerTree(ctx, node); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
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
