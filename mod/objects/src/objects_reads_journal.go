package objects

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// objectsReadsSink persists a batch of last-read times to durable storage.
type objectsReadsSink func(map[astral.ObjectID]astral.Time) error

// objectsReadsJournal records qualified object reads in memory and flushes them
// to a sink in batches. The hot read path (Mark) never touches the DB; flushing
// is caller-driven (Flush), wired to shutdown and purge.
type objectsReadsJournal struct {
	log *log.Logger
	mu  sync.Mutex

	pending map[astral.ObjectID]astral.Time // last read time per object
	sink    objectsReadsSink
}

func newObjectsReadsJournal(sink objectsReadsSink, log *log.Logger) *objectsReadsJournal {
	return &objectsReadsJournal{
		pending: map[astral.ObjectID]astral.Time{},
		sink:    sink,
		log:     log,
	}
}

// Mark records a qualified read of id at the current time. Hot path: O(1), no DB I/O.
func (j *objectsReadsJournal) Mark(id *astral.ObjectID) {
	if id == nil {
		return
	}
	j.mu.Lock()
	j.pending[*id] = astral.Now()
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
// drain hands each entry to exactly one flusher. On sink error the batch is
// re-merged so it isn't lost (keeping the newest time).
func (j *objectsReadsJournal) Flush() error {
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
