package objects

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// tracker keeps the tracking rows, the reads journal and the per-leaf locks of every leaf repository.
// The zero tracker registers leaves; attach gives it the database that tracking writes to.
// note: the rules are in VFS 6, §11 "objects: tracking": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
type tracker struct {
	db      *DB
	log     *log.Logger
	journal *objectsReadsJournal

	leaves sig.Map[string, objectsmod.Repository] // leaf name → the repository it wraps

	// locks holds one lock per leaf name. Commit, delete and purge of a leaf hold its lock.
	// why: purge holds the lock for each candidate from its re-check to its delete, so commit, delete and purge
	// never interleave, and a commit that answered cannot lose to a purge (VFS 6, §11 "objects: purge").
	// note: a lock outlives its leaf, so a writer of a removed leaf still serializes with its name.
	locks sig.Map[string, *sync.Mutex]
}

// attach gives the tracker its database and log, and a reads journal that flushes into the database.
func (t *tracker) attach(db *DB, log *log.Logger) {
	t.db = db
	t.log = log
	t.journal = newObjectsReadsJournal(db.UpdateReadAt, log)
}

// add records repo as the leaf named name.
func (t *tracker) add(name string, repo objectsmod.Repository) {
	t.leaves.Replace(name, repo)
}

// leaf returns the repository of the leaf named name.
func (t *tracker) leaf(name string) (objectsmod.Repository, bool) {
	return t.leaves.Get(name)
}

// leafLock returns the lock of the leaf named name.
func (t *tracker) leafLock(name string) *sync.Mutex {
	if mu, ok := t.locks.Get(name); ok {
		return mu
	}
	mu, _ := t.locks.Set(name, &sync.Mutex{})
	return mu
}

// committed writes the commit time of objectID in the leaf. The caller holds the leaf's lock.
// note: a failed tracking write leaves the commit successful and is logged; an untracked object is never purged.
// note: operator decision 2026-11-12, recorded in https://wiki.satforge.dev/doc/implement-the-vfs-module-and-the-changes-that-ship-with-it-oofFdNw03v
func (t *tracker) committed(leaf string, objectID *astral.ObjectID) {
	err := t.db.trackCommit(leaf, objectID, astral.Now().Time().Unix())
	if err != nil {
		t.log.Error("track %v in %v: %v", objectID, leaf, err)
	}
}

// deleteLocked deletes the object from repo, the leaf named leaf, and then removes its row. The caller holds the
// leaf's lock.
func (t *tracker) deleteLocked(ctx *astral.Context, leaf string, repo objectsmod.Repository, objectID *astral.ObjectID) error {
	resolved, err := resolveFullID(ctx, repo, objectID)
	if err != nil {
		return err
	}

	err = repo.Delete(ctx, objectID)
	if err != nil {
		return err
	}

	t.dropRow(leaf, resolved)
	return nil
}

// dropRow removes the row of objectID in the leaf. A failure is logged.
func (t *tracker) dropRow(leaf string, objectID *astral.ObjectID) {
	err := t.db.deleteTracking(leaf, objectID)
	if err != nil {
		t.log.Error("untrack %v in %v: %v", objectID, leaf, err)
	}
}

// forget removes every row of the leaf named name and stops tracking it.
func (t *tracker) forget(name string) {
	t.leaves.Delete(name)

	err := t.db.deleteRepositoryTracking(name)
	if err != nil {
		t.log.Error("untrack repository %v: %v", name, err)
	}
}

// openReader returns a reader of r that counts the read, unless ctx is housekeeping. offset is the offset r was
// opened at.
// note: the rules are in VFS 6, §11 "objects: last touched": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
func (t *tracker) openReader(ctx *astral.Context, r objectsmod.Reader, offset int64) objectsmod.Reader {
	cr := &countedReader{Reader: r, tracker: t}
	cr.counted.Store(!objectsmod.IsHousekeeping(ctx))

	// why: an object smaller than or equal to the offset delivers no byte, and the operator counts such a read when it closes.
	cr.zeroOnly = offset >= 0 && uint64(offset) >= r.ID().Size

	return cr
}

// markRead notes a counted read of objectID in the journal.
// note: marking does no database work.
func (t *tracker) markRead(objectID *astral.ObjectID) {
	t.journal.Mark(objectID)
}

// flush writes the reads journal.
func (t *tracker) flush() error {
	return t.journal.Flush()
}

// resolveFullID returns the full ID of the object a partial objectID names in repo.
// why: a row is keyed by the full ID, and only the repository knows which object a partial ID names.
func resolveFullID(ctx *astral.Context, repo objectsmod.Repository, objectID *astral.ObjectID) (*astral.ObjectID, error) {
	if objectID.Size != 0 {
		return objectID, nil
	}

	r, err := repo.Read(ctx, objectID, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return r.ID(), nil
}
