package objects

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// objectsReadsSink persists a batch of last-read times to durable storage.
type objectsReadsSink func(map[astral.ObjectID]astral.Time) error

// objectsReadsJournal records counted object reads in memory and flushes them
// to a sink in batches. The hot read path (Mark) never touches the DB; flushing
// is caller-driven (Flush), wired to the minute tick, purge and shutdown.
// note: the rules are in VFS 6, §11 "objects: last touched": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
type objectsReadsJournal struct {
	log *log.Logger
	mu  sync.Mutex

	// flushMu serializes Flush across drain and sink.
	// why: a flush that returns must find every read marked before it in the sink; a flush in flight
	// has already drained its batch, so a second flusher waits for it instead of finding an empty journal.
	flushMu sync.Mutex

	pending map[astral.ObjectID]astral.Time // last read time per object
	sink    objectsReadsSink

	// now is the journal's clock. Tests replace it.
	now func() time.Time
}

func newObjectsReadsJournal(sink objectsReadsSink, log *log.Logger) *objectsReadsJournal {
	return &objectsReadsJournal{
		pending: map[astral.ObjectID]astral.Time{},
		sink:    sink,
		log:     log,
		now:     time.Now,
	}
}

// Mark records a counted read of id at the current time. Hot path: O(1), no DB I/O.
func (j *objectsReadsJournal) Mark(id *astral.ObjectID) {
	if id == nil {
		return
	}
	j.mu.Lock()
	j.pending[*id] = astral.Time(j.now())
	j.mu.Unlock()
}

// drain atomically takes and clears the pending set, returning nil when empty.
func (j *objectsReadsJournal) drain() map[astral.ObjectID]astral.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.pending) == 0 {
		return nil
	}
	out := j.pending
	j.pending = map[astral.ObjectID]astral.Time{}
	return out
}

// Flush writes pending reads to the sink synchronously. Safe for concurrent use:
// flushes run one at a time, so when Flush returns nil every read marked before
// it was called is in the sink. On sink error the batch is re-merged so it isn't
// lost (keeping the newest time).
func (j *objectsReadsJournal) Flush() error {
	j.flushMu.Lock()
	defer j.flushMu.Unlock()

	batch := j.drain()
	if batch == nil {
		return nil
	}

	err := j.sink(batch)
	if err != nil {
		j.remerge(batch)
		return err
	}

	return nil
}

// remerge puts a failed batch back without clobbering newer marks.
func (j *objectsReadsJournal) remerge(batch map[astral.ObjectID]astral.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for k, v := range batch {
		cur, ok := j.pending[k]
		if !ok || v.Time().After(cur.Time()) {
			j.pending[k] = v
		}
	}
}
