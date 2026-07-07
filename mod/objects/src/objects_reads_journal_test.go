package objects

import (
	"errors"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
)

func TestObjectsReadsJournal_MarkThenFlush(t *testing.T) {
	var got map[astral.ObjectID]astral.Time
	j := newObjectsReadsJournal(func(b map[astral.ObjectID]astral.Time) error {
		got = b
		return nil
	}, nil)

	id := astral.ObjectID{Size: 1}
	j.Mark(&id)

	if err := j.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if _, ok := got[id]; !ok || len(got) != 1 {
		t.Fatalf("sink got %v, want exactly the marked id", got)
	}
	if len(j.pending) != 0 {
		t.Fatalf("pending not cleared after flush: %d", len(j.pending))
	}
}

func TestObjectsReadsJournal_MarkNilIsNoOp(t *testing.T) {
	j := newObjectsReadsJournal(func(map[astral.ObjectID]astral.Time) error { return nil }, nil)

	j.Mark(nil)

	if len(j.pending) != 0 {
		t.Fatalf("Mark(nil) recorded an entry: %d", len(j.pending))
	}
}

func TestObjectsReadsJournal_FlushEmptySkipsSink(t *testing.T) {
	j := newObjectsReadsJournal(func(map[astral.ObjectID]astral.Time) error {
		t.Fatal("sink called with no pending reads")
		return nil
	}, nil)

	if err := j.Flush(); err != nil {
		t.Fatalf("Flush on empty: %v", err)
	}
}

func TestObjectsReadsJournal_FlushErrorRemergesBatch(t *testing.T) {
	sinkErr := errors.New("sink down")
	j := newObjectsReadsJournal(func(map[astral.ObjectID]astral.Time) error {
		return sinkErr
	}, nil)

	id := astral.ObjectID{Size: 1}
	j.Mark(&id)

	err := j.Flush()
	if !errors.Is(err, sinkErr) {
		t.Fatalf("Flush err = %v, want %v", err, sinkErr)
	}
	if _, ok := j.pending[id]; !ok || len(j.pending) != 1 {
		t.Fatalf("failed batch not re-merged: pending=%v", j.pending)
	}
}

func TestObjectsReadsJournal_RemergeKeepsNewest(t *testing.T) {
	j := newObjectsReadsJournal(func(map[astral.ObjectID]astral.Time) error { return nil }, nil)

	id := astral.ObjectID{Size: 1}
	newer := astral.Time(time.Unix(200, 0))
	older := astral.Time(time.Unix(100, 0))

	// a newer mark arrived after the failed batch was taken; remerge must not clobber it
	j.pending[id] = newer
	j.remerge(map[astral.ObjectID]astral.Time{id: older})
	if got := j.pending[id]; !got.Time().Equal(newer.Time()) {
		t.Fatalf("remerge clobbered newer mark: got %v, want %v", got.Time(), newer.Time())
	}

	// when no current mark exists, remerge restores the batch entry
	delete(j.pending, id)
	j.remerge(map[astral.ObjectID]astral.Time{id: older})
	if got, ok := j.pending[id]; !ok || !got.Time().Equal(older.Time()) {
		t.Fatalf("remerge did not restore missing entry: ok=%v got=%v", ok, got.Time())
	}
}
