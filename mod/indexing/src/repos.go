package indexing

import (
	"errors"
	"fmt"
	modindexingmod "github.com/astralp2p/astrald/mod/indexing"

	modindexing "github.com/astralp2p/astral-go/api/indexing"
	"github.com/astralp2p/astral-go/astral"
)

// EnableRepo persists repoName in the tree and launches its sync goroutine.
func (mod *Module) EnableRepo(ctx *astral.Context, repoName string) error {
	_, err := mod.repos.Create(ctx, repoName)
	if err != nil {
		return err
	}

	return mod.startRepoSync(repoName)
}

// DisableRepo cancels the repo's sync goroutine and removes it from the
// persisted tree; no-ops silently if the tree node is absent.
func (mod *Module) DisableRepo(ctx *astral.Context, repoName string) error {
	mod.stopRepoSync(repoName)

	sub, err := mod.repos.Sub(ctx)
	if err != nil {
		return err
	}

	del, ok := sub[repoName]
	if ok {
		del.Delete(ctx)
	}

	return nil
}

// enabledRepos lists currently-syncing repo names.
func (mod *Module) enabledRepos() []string {
	return mod.syncing.Keys()
}

func (mod *Module) startRepoSync(repoName string) error {
	ctx, cancel := mod.ctx.WithCancel()

	_, ok := mod.syncing.Set(repoName, cancel)
	if !ok {
		cancel()
		return modindexingmod.ErrRepoAlreadySyncing
	}

	go func() {
		mod.log.Logv(1, "following repo %v", repoName)
		err := mod.syncRepo(ctx, repoName)
		if err != nil {
			mod.log.Logv(1, "error syncing repo %v: %v", repoName, err)
		} else {
			mod.log.Logv(1, "stopped following %v", repoName)
		}
	}()

	return nil
}

func (mod *Module) stopRepoSync(repoName string) error {
	cancel, ok := mod.syncing.Delete(repoName)
	if !ok {
		return modindexingmod.ErrRepoNotSyncing
	}
	cancel()
	return nil
}

func (mod *Module) syncRepo(ctx *astral.Context, repoName string) error {
	repo := mod.Objects.GetRepository(repoName)
	if repo == nil {
		return modindexing.ErrRepositoryNotFound
	}

	scan, err := repo.Scan(ctx, true)
	if err != nil {
		return err
	}

	// take a snapshot
	var snapshot []*astral.ObjectID

	for {
		select {
		case objectID, ok := <-scan:
			if !ok {
				return fmt.Errorf("repo %q scan ended without snapshot boundary", repoName)
			}

			if objectID == nil {
				goto snapshot
			}
			snapshot = append(snapshot, objectID)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

snapshot:

	var removed, added int

	// remove objects from the index that are not in the snapshot
	excess, err := mod.db.findExcessObjectIDs(repoName, snapshot)
	if err != nil {
		return err
	}

	for _, objectID := range excess {
		removed++
		err = mod.db.removeFromRepo(repoName, objectID)
		if err != nil {
			mod.log.Logv(1, "db error removing from repo: %v", err)
			continue
		}
		mod.broadcastChange()

		// check context
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// add objects from the snapshot that are not in the index
	missing, err := mod.db.findMissingObjectIDs(repoName, snapshot)
	if err != nil {
		return err
	}
	for _, objectID := range missing {
		added++
		err = mod.db.addToRepo(repoName, objectID)
		if err != nil {
			mod.log.Logv(1, "error adding %v to repo %v: %v", objectID, repoName, err)
			continue
		}
		mod.broadcastChange()

		// check context
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	mod.log.Logv(1, "index synced with repo %v: %v removed, %v added.", repoName, removed, added)

	// follow updates from the repo until ctx is canceled
	for objectID := range scan {
		if objectID == nil {
			return fmt.Errorf("repo %q emitted duplicate snapshot boundary", repoName)
		}

		err = mod.db.addToRepo(repoName, objectID)
		if err != nil {
			if errors.Is(err, modindexingmod.ErrObjectAlreadyAdded) {
				continue
			}
			mod.log.Logv(1, "db error adding to repo: %v", err)
			continue
		}
		mod.broadcastChange()
	}

	return nil
}

// changeSignal returns a channel that will be closed when a new change is appended.
// Each call returns the current pending signal; once fired, callers re-fetch.
func (mod *Module) changeSignal() <-chan struct{} {
	mod.notifyMu.Lock()
	defer mod.notifyMu.Unlock()
	if mod.notify == nil {
		mod.notify = make(chan struct{})
	}
	return mod.notify
}

// broadcastChange wakes every waiter blocked on changeSignal.
func (mod *Module) broadcastChange() {
	mod.notifyMu.Lock()
	defer mod.notifyMu.Unlock()
	if mod.notify != nil {
		close(mod.notify)
	}
	mod.notify = make(chan struct{})
}
