package objects

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/fs"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

// fakeClock is a settable clock for the reads journal.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newClockedModule returns a tracking module whose journal runs on a fake clock.
func newClockedModule(t *testing.T) (*Module, *fakeClock) {
	t.Helper()

	mod := newTrackingModule(t)
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	mod.objectsReadsJournal.now = clock.now
	return mod, clock
}

// pendingTime returns the journal's pending read time of id.
func pendingTime(mod *Module, id *astral.ObjectID) (time.Time, bool) {
	j := mod.objectsReadsJournal
	j.mu.Lock()
	defer j.mu.Unlock()

	at, ok := j.pending[*id]
	return at.Time(), ok
}

// expectNotPending fails when the journal holds a read of id.
func expectNotPending(t *testing.T, mod *Module, id *astral.ObjectID, when string) {
	t.Helper()

	if at, ok := pendingTime(mod, id); ok {
		t.Fatalf("journal holds a read of %v at %v %v; want none", id, at, when)
	}
}

// expectPending fails unless the journal holds a read of id at want.
func expectPending(t *testing.T, mod *Module, id *astral.ObjectID, want time.Time, when string) {
	t.Helper()

	at, ok := pendingTime(mod, id)
	if !ok {
		t.Fatalf("journal holds no read of %v %v; want one at %v", id, when, want)
	}
	if !at.Equal(want) {
		t.Fatalf("journal read time of %v %v is %v; want %v", id, when, at, want)
	}
}

// openReader opens id in repo at offset with no limit.
func openReader(t *testing.T, ctx *astral.Context, repo objectsmod.Repository, id *astral.ObjectID, offset int64) objectsmod.Reader {
	t.Helper()

	r, err := repo.Read(ctx, id, offset, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return r
}

// TestReadCounting_FirstByteCounts: a read counts when its first byte is delivered, not when it opens.
func TestReadCounting_FirstByteCounts(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	id := commitData(t, ctx, repo, "first byte")

	r := openReader(t, ctx, repo, id, 0)
	defer r.Close()
	expectNotPending(t, mod, id, "after open")

	clock.advance(5 * time.Second)
	if _, err := r.Read(make([]byte, 1)); err != nil {
		t.Fatalf("read a byte: %v", err)
	}
	expectPending(t, mod, id, clock.t, "after the first byte")
}

// TestReadCounting_OpenCloseWithoutBytesDoesNotCount: a reader closed before it delivers a byte is not counted.
func TestReadCounting_OpenCloseWithoutBytesDoesNotCount(t *testing.T) {
	mod, _ := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	id := commitData(t, ctx, repo, "never read")

	r := openReader(t, ctx, repo, id, 0)
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	expectNotPending(t, mod, id, "after open and close")

	// a window with a limit is not a zero-byte read either
	limited, err := repo.Read(ctx, id, 2, 3)
	if err != nil {
		t.Fatalf("read window: %v", err)
	}
	limited.Close()
	expectNotPending(t, mod, id, "after a closed window with bytes")
}

// TestReadCounting_ZeroByteReadCountsOnClose: a read that can deliver only zero bytes counts when it closes.
func TestReadCounting_ZeroByteReadCountsOnClose(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	data := "end of object"
	id := commitData(t, ctx, repo, data)

	r := openReader(t, ctx, repo, id, int64(len(data)))
	n, err := r.Read(make([]byte, 8))
	if n != 0 || err != io.EOF {
		t.Fatalf("read at the end: %d, %v; want 0, EOF", n, err)
	}
	expectNotPending(t, mod, id, "before close")

	clock.advance(time.Second)
	r.Close()
	expectPending(t, mod, id, clock.t, "after close")

	// a second close counts nothing more
	clock.advance(time.Second)
	r.Close()
	expectPending(t, mod, id, clock.t.Add(-time.Second), "after a second close")
}

// TestReadCounting_LongStreamIsCountedAgainAfterAMinute: a stream that keeps delivering bytes is counted once a minute.
func TestReadCounting_LongStreamIsCountedAgainAfterAMinute(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	id := commitData(t, ctx, repo, "a long stream of bytes")

	r := openReader(t, ctx, repo, id, 0)
	defer r.Close()

	buf := make([]byte, 1)
	r.Read(buf)
	first := clock.t
	expectPending(t, mod, id, first, "after the first byte")

	clock.advance(59 * time.Second)
	r.Read(buf)
	expectPending(t, mod, id, first, "59 seconds later")

	clock.advance(time.Second)
	r.Read(buf)
	expectPending(t, mod, id, clock.t, "a minute later")

	again := clock.t
	clock.advance(30 * time.Second)
	r.Read(buf)
	expectPending(t, mod, id, again, "30 seconds after the second count")
}

// TestReadCounting_HousekeepingReadRecordsNothing: a read under the housekeeping mark leaves the journal empty.
func TestReadCounting_HousekeepingReadRecordsNothing(t *testing.T) {
	mod, _ := newClockedModule(t)
	repo := mod.GetRepository("mem0")
	id := commitData(t, astral.NewContext(nil), repo, "housekeeping read")
	empty := commitData(t, astral.NewContext(nil), repo, "x")

	ctx := objectsmod.WithHousekeeping(astral.NewContext(nil))
	r := openReader(t, ctx, repo, id, 0)
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("read all: %v", err)
	}
	r.Close()

	// a zero-byte housekeeping read records nothing on close either
	z := openReader(t, ctx, repo, empty, 1)
	z.Close()

	if _, err := mod.Probe(astral.NewContext(nil), repo, id); err != nil {
		t.Fatalf("probe: %v", err)
	}

	if n := len(mod.objectsReadsJournal.pending); n != 0 {
		t.Fatalf("journal holds %d reads after housekeeping reads; want none", n)
	}
}

// TestReadCounting_FlushKeepsNewerStoredTime: a flush only raises a read time.
func TestReadCounting_FlushKeepsNewerStoredTime(t *testing.T) {
	mod, _ := newClockedModule(t)
	id := commitData(t, astral.NewContext(nil), mod.GetRepository("mem0"), "stored time")

	err := mod.db.Model(&dbTracking{}).Where("repository = ?", "mem0").Update("read_at", 500).Error
	if err != nil {
		t.Fatalf("set the stored time: %v", err)
	}

	j := mod.objectsReadsJournal
	j.pending[*id] = astral.Time(time.Unix(100, 0))
	if err := j.Flush(); err != nil {
		t.Fatalf("flush older: %v", err)
	}
	if got := expectTracked(t, mod, "mem0", id).ReadAt; got != 500 {
		t.Fatalf("read time %d after a flush of an older time; want 500", got)
	}

	j.pending[*id] = astral.Time(time.Unix(500, 0))
	if err := j.Flush(); err != nil {
		t.Fatalf("flush equal: %v", err)
	}
	if got := expectTracked(t, mod, "mem0", id).ReadAt; got != 500 {
		t.Fatalf("read time %d after a flush of an equal time; want 500", got)
	}

	j.pending[*id] = astral.Time(time.Unix(900, 0))
	if err := j.Flush(); err != nil {
		t.Fatalf("flush newer: %v", err)
	}
	if got := expectTracked(t, mod, "mem0", id).ReadAt; got != 900 {
		t.Fatalf("read time %d after a flush of a newer time; want 900", got)
	}
}

// TestReadCounting_FlushNeverCreatesRow: a journal entry of an untracked object changes nothing.
func TestReadCounting_FlushNeverCreatesRow(t *testing.T) {
	mod, _ := newClockedModule(t)
	tracked := commitData(t, astral.NewContext(nil), mod.GetRepository("mem0"), "tracked")

	inner := mem.New("leaf", 0)
	untracked := commitData(t, astral.NewContext(nil), inner, "untracked")

	j := mod.objectsReadsJournal
	j.Mark(untracked)
	j.Mark(&astral.ObjectID{Size: 7})
	if err := j.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var count int64
	if err := mod.db.Model(&dbTracking{}).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d rows after a flush of untracked objects; want 1", count)
	}
	if got := expectTracked(t, mod, "mem0", tracked).ReadAt; got != 0 {
		t.Fatalf("read time %d of an object not in the journal; want 0", got)
	}
}

// TestReadCounting_FlushSetsEveryCopy: one counted read sets the read time of every row with that ObjectID.
func TestReadCounting_FlushSetsEveryCopy(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	id := commitData(t, ctx, mod.GetRepository("mem0"), "two copies")
	commitData(t, ctx, mod.System(), "two copies")
	other := commitData(t, ctx, mod.GetRepository("mem0"), "other object")

	r := openReader(t, ctx, mod.GetRepository("mem0"), id, 0)
	io.ReadAll(r)
	r.Close()
	if err := mod.objectsReadsJournal.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	want := clock.t.Unix()
	if got := expectTrackedID(t, mod, "mem0", id).ReadAt; got != want {
		t.Errorf("read time in mem0 is %d; want %d", got, want)
	}
	if got := expectTrackedID(t, mod, "system", id).ReadAt; got != want {
		t.Errorf("read time in system is %d; want %d", got, want)
	}
	if got := expectTrackedID(t, mod, "mem0", other).ReadAt; got != 0 {
		t.Errorf("read time of the other object is %d; want 0", got)
	}
}

// expectTrackedID returns the row of id in the leaf named repo, and fails when there is none.
func expectTrackedID(t *testing.T, mod *Module, repo string, id *astral.ObjectID) *dbTracking {
	t.Helper()

	for _, row := range trackingRows(t, mod, repo) {
		if row.ObjectID.IsEqual(id) {
			return row
		}
	}
	t.Fatalf("no tracking row of %v in %v", id, repo)
	return nil
}

// TestReadCounting_SystemAndGroupReadsCount: a read through System() and through a group that ends in a leaf counts.
func TestReadCounting_SystemAndGroupReadsCount(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)

	systemID := commitData(t, ctx, mod.System(), "system object")
	groupID := commitData(t, ctx, mod.GetRepository("mem0"), "group object")

	r := openReader(t, ctx, mod.System(), systemID, 0)
	io.ReadAll(r)
	r.Close()
	expectPending(t, mod, systemID, clock.t, "after a read through System()")

	for _, name := range []string{"main", "device", "local"} {
		group := mod.GetRepository(name)
		if _, ok := group.(*RepoGroup); !ok {
			t.Fatalf("%v is %T; want a group", name, group)
		}
	}

	clock.advance(time.Second)
	r = openReader(t, ctx, mod.GetRepository("main"), groupID, 0)
	if r.Repo() != mod.GetRepository("mem0") {
		t.Fatalf("the group reader names %v; want the wrapper of mem0", r.Repo().Label())
	}
	io.ReadAll(r)
	r.Close()
	expectPending(t, mod, groupID, clock.t, "after a read through the main group")
}

// plainReader hides every method of its reader except the Reader interface.
type plainReader struct{ objectsmod.Reader }

// plainLeaf is a leaf whose readers cannot seek.
type plainLeaf struct{ *mem.Repository }

func (r *plainLeaf) Read(ctx *astral.Context, id *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	inner, err := r.Repository.Read(ctx, id, offset, limit)
	if err != nil {
		return nil, err
	}
	return &plainReader{Reader: inner}, nil
}

// TestReadCounting_SeekerOnlyWhereInnerSeeks: the wrapper claims io.Seeker only for an inner reader that seeks.
func TestReadCounting_SeekerOnlyWhereInnerSeeks(t *testing.T) {
	mod, _ := newClockedModule(t)
	ctx := astral.NewContext(nil)

	plain := &plainLeaf{Repository: mem.New("plain", 0)}
	id := commitData(t, ctx, plain, "no seek")
	if err := mod.AddRepository("plain", plain); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if err := mod.AddRepository("seeking", mem.New("seeking", 0)); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	commitData(t, ctx, mod.GetRepository("seeking"), "no seek")

	r := openReader(t, ctx, mod.GetRepository("plain"), id, 0)
	defer r.Close()
	if _, ok := r.(io.Seeker); ok {
		t.Fatal("the wrapper of a reader that cannot seek claims io.Seeker")
	}
	if r.Repo() != mod.GetRepository("plain") {
		t.Fatal("the reader does not name the wrapper")
	}

	s := openReader(t, ctx, mod.GetRepository("seeking"), id, 0)
	defer s.Close()
	if _, ok := s.(io.Seeker); !ok {
		t.Fatal("the wrapper of a reader that has Seek does not claim io.Seeker")
	}
	if s.Repo() != mod.GetRepository("seeking") {
		t.Fatal("the seeking reader does not name the wrapper")
	}
}

// TestReadCounting_ReadSeekerAndFSPathsCount: reads through NewReadSeeker and the object file system count, and Seek reopens.
func TestReadCounting_ReadSeekerAndFSPathsCount(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	data := "0123456789"
	id := commitData(t, ctx, repo, data)

	// the object file system serves the HTTP object handler
	f, err := fs.NewFS(repo).Open(id.String())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	expectNotPending(t, mod, id, "after the file system open")

	seeker, ok := f.(io.Seeker)
	if !ok {
		t.Fatal("the opened file does not seek")
	}
	if pos, err := seeker.Seek(4, io.SeekStart); err != nil || pos != 4 {
		t.Fatalf("seek: %d, %v; want 4, nil", pos, err)
	}
	clock.advance(time.Second)
	got := make([]byte, 3)
	if _, err := io.ReadFull(f, got); err != nil || string(got) != "456" {
		t.Fatalf("read after seek: %q, %v; want 456", got, err)
	}
	f.Close()
	expectPending(t, mod, id, clock.t, "after a read behind a seek")

	// a reader opened by NewReadSeeker and closed without a byte is not counted
	other := commitData(t, ctx, repo, "unread")
	rs := objectsmod.NewReadSeeker(ctx, other, repo, nil)
	if _, err := rs.Seek(2, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	rs.Close()
	expectNotPending(t, mod, other, "after a seek and close")
}

// TestReadCounting_RunFlushesEveryInterval: Run's loop writes a non-empty journal on each tick.
func TestReadCounting_RunFlushesEveryInterval(t *testing.T) {
	mod, _ := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	id := commitData(t, ctx, repo, "ticked")

	runCtx, cancel := ctx.WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.runJournalFlush(runCtx, 5*time.Millisecond)
	}()
	defer func() {
		cancel()
		<-done
	}()

	r := openReader(t, ctx, repo, id, 0)
	io.ReadAll(r)
	r.Close()

	deadline := time.Now().Add(5 * time.Second)
	for expectTrackedID(t, mod, "mem0", id).ReadAt == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the loop did not flush the journal on a tick")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestReadCounting_RunFlushesAtShutdown: Run's loop writes the journal when its context ends, with no tick in between.
func TestReadCounting_RunFlushesAtShutdown(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	id := commitData(t, ctx, repo, "late")

	runCtx, cancel := ctx.WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.runJournalFlush(runCtx, time.Hour)
	}()

	clock.advance(time.Hour)
	r := openReader(t, ctx, repo, id, 0)
	io.ReadAll(r)
	r.Close()
	if got := expectTrackedID(t, mod, "mem0", id).ReadAt; got != 0 {
		t.Fatalf("read time %d before shutdown; want none", got)
	}

	cancel()
	<-done
	if got := expectTrackedID(t, mod, "mem0", id).ReadAt; got != clock.t.Unix() {
		t.Fatalf("read time %d after the final flush; want %d", got, clock.t.Unix())
	}
}

// TestReadCounting_CandidateCountsOnlyAfterCount: a read opened as housekeeping delivers bytes uncounted until count is called.
func TestReadCounting_CandidateCountsOnlyAfterCount(t *testing.T) {
	mod, clock := newClockedModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	data := "candidate"
	id := commitData(t, ctx, repo, data)
	hk := objectsmod.WithHousekeeping(ctx)

	// a loser delivers its head and closes
	loser := openReader(t, hk, repo, id, 0)
	if _, err := loser.Read(make([]byte, 4)); err != nil {
		t.Fatalf("read: %v", err)
	}
	loser.Close()
	expectNotPending(t, mod, id, "after a losing candidate")

	// a winner counts the head it delivered when it is told to count
	clock.advance(time.Second)
	winner := openReader(t, hk, repo, id, 0)
	if _, err := winner.Read(make([]byte, 4)); err != nil {
		t.Fatalf("read: %v", err)
	}
	winner.(interface{ count() }).count()
	expectPending(t, mod, id, clock.t, "after the winner counted")
	winner.Close()

	// a winner with no byte counts when it closes
	other := commitData(t, ctx, repo, "empty window")
	clock.advance(time.Second)
	zero := openReader(t, hk, repo, other, int64(len("empty window")))
	zero.(interface{ count() }).count()
	expectNotPending(t, mod, other, "before the zero-byte winner closes")
	zero.Close()
	expectPending(t, mod, other, clock.t, "after the zero-byte winner closed")
}
