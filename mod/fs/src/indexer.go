package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/lib/paths"
	"github.com/astralp2p/astrald/mod/fs"
	"golang.org/x/time/rate"
)

// todo: add more extensive logging

const (
	statRate = 500
	hashRate = 100

	statBurst = 2000
	hashBurst = 300

	workqueueSize = 50000
	enqueueRate   = 800
	enqueueBurst  = 5000
)

type IndexEvent struct {
	Path     string
	ObjectID *astral.ObjectID
}

type fileEntry struct {
	path    string
	modTime int64
}

// Indexer tracks filesystem paths in a database, hashing files to derive object IDs.
// It manages a bounded work queue, per-root scope, I/O rate limits, and an event fan-out for subscribers.
type Indexer struct {
	mod *Module
	log *log.Logger

	workqueue chan string
	pending   sig.Set[string]

	roots sig.Set[string]

	mu              sync.Mutex
	events          *sig.Queue[IndexEvent]
	subscriberCount int

	// rate limiters to prevent too many I/O operations
	statLimiter    *rate.Limiter
	hashLimiter    *rate.Limiter
	enqueueLimiter *rate.Limiter
}

func NewIndexer(mod *Module) *Indexer {
	indexer := &Indexer{
		mod:       mod,
		log:       mod.log,
		workqueue: make(chan string, workqueueSize),
		events:    &sig.Queue[IndexEvent]{},

		statLimiter:    rate.NewLimiter(rate.Limit(statRate), statBurst),
		hashLimiter:    rate.NewLimiter(rate.Limit(hashRate), hashBurst),
		enqueueLimiter: rate.NewLimiter(rate.Limit(enqueueRate), enqueueBurst),
	}

	return indexer
}

func (indexer *Indexer) startWorkers(ctx *astral.Context, count int) {
	for i := 0; i < count; i++ {
		go indexer.worker(ctx)
	}
}

func (indexer *Indexer) worker(ctx *astral.Context) {
	const flushDelay = 5 * time.Second

	softDeletes := NewBatchCollector(1000, indexer.mod.db.SoftDeletePaths)
	timer := time.NewTimer(flushDelay)
	timer.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := softDeletes.Flush(); err != nil {
				indexer.log.Error("indexer: batch delete: %v", err)
			}

			return
		case <-timer.C:
			if err := softDeletes.Flush(); err != nil {
				indexer.log.Error("indexer: batch delete: %v", err)
			}

			timer.Reset(flushDelay)
		case path := <-indexer.workqueue:
			indexer.pending.Remove(path)

			var found bool
			for _, root := range indexer.roots.Clone() {
				if paths.PathUnder(path, root, filepath.Separator) {
					found = true
					break
				}
			}

			if !found {
				if err := softDeletes.Add(path); err != nil {
					indexer.log.Error("indexer: batch delete: %v", err)
				}
				timer.Reset(flushDelay)
				continue
			}

			if err := indexer.checkAndFix(ctx, path); err != nil {
				indexer.log.Error("indexer: checkAndFix %v: %v", path, err)
			}
		}
	}
}

func (indexer *Indexer) deletePath(path string) (err error) {
	err = indexer.mod.db.DeletePath(path)
	if err != nil {
		return fmt.Errorf("db delete: %w", err)
	}

	return nil
}

func (indexer *Indexer) enqueue(path string) {
	if err := indexer.pending.Add(path); err == nil {
		indexer.workqueue <- path
	}
}

func (indexer *Indexer) requeuePath(path string) (err error) {
	err = indexer.mod.db.InvalidatePath(path)
	if err != nil {
		return fmt.Errorf("db invalidate path: %w", err)
	}

	indexer.enqueue(path)
	return
}

func (indexer *Indexer) checkAndFix(ctx context.Context, path string) error {
	if len(path) == 0 || path[0] != '/' {
		return fs.ErrInvalidPath
	}

	if err := indexer.statLimiter.Wait(ctx); err != nil {
		return err
	}

	stat, err := os.Stat(path)
	if err != nil {
		// File was not found at filesystem -> delete from database
		err = indexer.mod.db.DeletePath(path)
		if err != nil {
			return fmt.Errorf("db delete: %w", err)
		}

		return nil
	}

	indexEntry, err := indexer.mod.db.FindByPath(path)
	if err == nil && indexEntry.ModTime == stat.ModTime().UnixNano() {
		// between index and filesystem nothing changed -> mark index as good
		return indexer.mod.db.ValidatePath(path)
	}

	if err := indexer.hashLimiter.Wait(ctx); err != nil {
		return err
	}

	objectID, err := resolveFileID(path)
	if err != nil {
		return fmt.Errorf("resolve ObjectID: %w", err)
	}

	// we calculated new objectID -> update database index
	err = indexer.mod.db.IndexPath(path, objectID, stat.ModTime().UnixNano())
	if err != nil {
		return fmt.Errorf("db upsert: %w", err)
	}

	indexer.mu.Lock()
	if indexer.subscriberCount > 0 {
		indexer.events = indexer.events.Push(IndexEvent{
			Path:     path,
			ObjectID: objectID,
		})
	}
	indexer.mu.Unlock()

	return nil
}

// init scans widest roots, inserts all paths as invalid into the database, invalidates whole db state and enqueues all paths for indexing
func (indexer *Indexer) init(ctx *astral.Context) error {
	now := time.Now()

	// discover new files
	for _, root := range paths.WidestRoots(indexer.roots.Clone()) {
		if err := indexer.scan(ctx, root, false); err != nil {
			return err
		}
	}

	indexer.mod.log.Info(`fs indexer: scan completed in %v`, time.Since(now))
	if err := indexer.mod.db.InvalidateAllPaths(); err != nil {
		return fmt.Errorf("invalidate all paths failed: %w", err)
	}

	enqueuer := NewBatchCollector(1000, func(batch []string) error {
		if err := indexer.enqueueLimiter.WaitN(ctx, len(batch)); err != nil {
			return err
		}
		for _, path := range batch {
			indexer.enqueue(path)
		}
		return nil
	})

	defer func() {
		err := enqueuer.Flush()
		if err != nil {
			indexer.log.Error("indexer: batch enqueue: %v", err)
		}
	}()

	return enqueuer.Iter(indexer.mod.db.EachInvalidatedPath)
}

// scan walks the filesystem from root and updates the database.
// Files with unchanged modtime are validated directly, others are invalidated and optionally enqueued.
func (indexer *Indexer) scan(ctx context.Context, root string, enqueue bool) error {
	collector := NewBatchCollector(1000, func(batch []fileEntry) error {
		batchPaths := make([]string, len(batch))
		for i, e := range batch {
			batchPaths[i] = e.path
		}

		existing, err := indexer.mod.db.LookupPaths(batchPaths)
		if err != nil {
			return fmt.Errorf("db find: %w", err)
		}

		var toValidate, toInvalidate []string
		for _, e := range batch {
			if ex, ok := existing[e.path]; ok && ex.ModTime == e.modTime {
				toValidate = append(toValidate, e.path)
			} else {
				toInvalidate = append(toInvalidate, e.path)
			}
		}

		if err := indexer.mod.db.ValidatePaths(toValidate); err != nil {
			return fmt.Errorf("db validate: %w", err)
		}

		if err := indexer.mod.db.InvalidatePaths(toInvalidate); err != nil {
			return fmt.Errorf("db invalidate: %w", err)
		}

		// Optionally enqueue invalidated paths for hashing
		if enqueue && len(toInvalidate) > 0 {
			if err := indexer.enqueueLimiter.WaitN(ctx, len(toInvalidate)); err != nil {
				return err
			}
			for _, path := range toInvalidate {
				indexer.enqueue(path)
			}
		}

		return nil
	})

	return collector.Iter(func(yield func(fileEntry) error) error {
		return paths.WalkDir(ctx, root, func(path string, info os.FileInfo) error {
			if err := indexer.statLimiter.Wait(ctx); err != nil {
				return err
			}

			return yield(fileEntry{path: path, modTime: info.ModTime().UnixNano()})
		})
	})
}

func (indexer *Indexer) addRoot(root string) error {
	return indexer.roots.Add(filepath.Clean(root))
}

func (indexer *Indexer) removeRoot(root string) error {
	root = filepath.Clean(root)
	roots := indexer.roots.Clone()

	// no deletion needed (still covered by other roots)
	for _, other := range roots {
		if other != root && paths.PathUnder(root, other, filepath.Separator) {
			return indexer.roots.Remove(root)
		}
	}

	// Find narrower roots under the root being removed
	var narrower []string
	for _, other := range roots {
		if other != root && paths.PathUnder(other, root, filepath.Separator) {
			narrower = append(narrower, other)
		}
	}

	// Build trie from narrower roots for O(L) coverage check
	trie, err := paths.NewPathTrie(narrower, filepath.Separator)
	if err != nil {
		if errors.Is(err, paths.ErrNotAbsolute) {
			return fs.ErrNotAbsolute
		}
		return fmt.Errorf("build trie: %w", err)
	}

	// Delete paths not covered by narrower roots
	deletes := NewBatchCollector(1000, indexer.mod.db.SoftDeletePaths)
	err = deletes.Iter(func(yield func(string) error) error {
		return indexer.mod.db.EachPath(root, func(path string) error {
			covered, err := trie.Covers(path)
			if err != nil {
				if errors.Is(err, paths.ErrNotAbsolute) {
					return fs.ErrNotAbsolute
				}
				return err
			}
			if !covered {
				return yield(path)
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("soft delete paths: %w", err)
	}

	return indexer.roots.Remove(root)
}

func (indexer *Indexer) subscribe() *sig.Queue[IndexEvent] {
	indexer.mu.Lock()
	defer indexer.mu.Unlock()
	indexer.subscriberCount++
	return indexer.events
}

func (indexer *Indexer) unsubscribe() {
	indexer.mu.Lock()
	defer indexer.mu.Unlock()
	indexer.subscriberCount--
}
