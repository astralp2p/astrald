package vfs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// fakeClock is a clock that moves only when a test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clk *fakeClock
	at  time.Time
	ch  chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_800_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) clockTimer {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := &fakeTimer{clk: c, at: c.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now
		return t
	}
	c.timers = append(c.timers, t)

	return t
}

// Advance moves the clock and fires every timer that is due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
	c.timers = slices.DeleteFunc(c.timers, func(t *fakeTimer) bool {
		if t.at.After(c.now) {
			return false
		}
		t.ch <- c.now
		return true
	})
}

// Timers returns the number of timers that wait.
func (c *fakeClock) Timers() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.timers)
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()

	n := len(t.clk.timers)
	t.clk.timers = slices.DeleteFunc(t.clk.timers, func(o *fakeTimer) bool { return o == t })

	return len(t.clk.timers) < n
}

// dirTestCommit is one commit of the test repository.
type dirTestCommit struct {
	ID    astral.ObjectID
	Bytes []byte
	At    time.Time
}

// dirTestLeaf is the lock a leaf repository holds across a commit or a delete.
type dirTestLeaf struct{ mu sync.Mutex }

// dirTestRepo is a repository in memory that records its commits and deletes.
//
// A commit and a delete hold the leaf lock and then use the module's database, as the tracking wrapper of the
// objects module does, so a store inside a transaction blocks here as it does on the node.
type dirTestRepo struct {
	objectsmod.Repository
	db    *DB
	clock clock
	leaf  dirTestLeaf

	mu         sync.Mutex
	objects    map[astral.ObjectID][]byte
	commits    []dirTestCommit
	deletes    []astral.ObjectID
	failCommit bool
	onCommit   func(id astral.ObjectID) error // runs before the commit, outside every lock; an error fails it
	active     int
	maxActive  int
}

func newDirTestRepo(db *DB, clk clock) *dirTestRepo {
	return &dirTestRepo{db: db, clock: clk, objects: map[astral.ObjectID][]byte{}}
}

func (r *dirTestRepo) Label() string { return "dirtest" }

func (r *dirTestRepo) Create(*astral.Context, *objectsmod.CreateOpts) (objectsapi.Writer, error) {
	return &dirTestWriter{repo: r}, nil
}

func (r *dirTestRepo) Contains(_ *astral.Context, id *astral.ObjectID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.objects[*id]

	return ok, nil
}

func (r *dirTestRepo) Delete(_ *astral.Context, id *astral.ObjectID) error {
	r.leaf.mu.Lock()
	defer r.leaf.mu.Unlock()

	return r.deleteLocked(id)
}

// deleteLocked deletes an object. The caller holds the leaf lock.
func (r *dirTestRepo) deleteLocked(id *astral.ObjectID) error {
	if err := r.db.Exec("SELECT 1").Error; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.objects[*id]; !ok {
		return objectsmod.ErrNotFound
	}
	delete(r.objects, *id)
	r.deletes = append(r.deletes, *id)

	return nil
}

func (r *dirTestRepo) commit(data []byte) (*astral.ObjectID, error) {
	id := astral.ObjectID{Size: uint64(len(data)), Hash: sha256.Sum256(data)}

	r.mu.Lock()
	hook, fail := r.onCommit, r.failCommit
	r.active++
	r.maxActive = max(r.maxActive, r.active)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.active--
	}()

	var hookErr error
	if hook != nil {
		hookErr = hook(id)
	}
	if fail || hookErr != nil {
		return nil, errors.New("commit failed")
	}

	r.leaf.mu.Lock()
	defer r.leaf.mu.Unlock()

	if err := r.db.Exec("SELECT 1").Error; err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.objects[id] = data
	r.commits = append(r.commits, dirTestCommit{ID: id, Bytes: data, At: r.clock.Now()})

	return &id, nil
}

// directoryCommits returns the commits of directories.
func (r *dirTestRepo) directoryCommits() (out []dirTestCommit) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, c := range r.commits {
		if isDirectory(c.Bytes) {
			out = append(out, c)
		}
	}

	return out
}

func (r *dirTestRepo) has(id astral.ObjectID) bool {
	ok, _ := r.Contains(nil, &id)
	return ok
}

func (r *dirTestRepo) setFailCommit(fail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failCommit = fail
}

func (r *dirTestRepo) setOnCommit(fn func(astral.ObjectID) error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.onCommit = fn
}

type dirTestWriter struct {
	repo *dirTestRepo
	buf  bytes.Buffer
}

func (w *dirTestWriter) Write(p []byte) (int, error)       { return w.buf.Write(p) }
func (w *dirTestWriter) Commit() (*astral.ObjectID, error) { return w.repo.commit(w.buf.Bytes()) }
func (w *dirTestWriter) Discard() error                    { return nil }

// dirTestObjects answers the default write repository and the holders: the module and any extra holder.
type dirTestObjects struct {
	objectsmod.Module
	repo  *dirTestRepo
	mu    sync.Mutex
	extra []objectsmod.Holder
}

func (o *dirTestObjects) WriteDefault() objectsmod.Repository {
	if o.repo == nil {
		return nil
	}

	return o.repo
}

func (o *dirTestObjects) Holders(id *astral.ObjectID) (out []objectsmod.Holder) {
	o.mu.Lock()
	holders := slices.Clone(o.extra)
	o.mu.Unlock()

	for _, h := range holders {
		if h.HoldObject(id) {
			out = append(out, h)
		}
	}

	return out
}

// DeleteUnheld deletes an object under the leaf lock unless a holder holds it or a commit of it is after since,
// as the objects module does.
func (o *dirTestObjects) DeleteUnheld(_ *astral.Context, _ objectsmod.Repository, id *astral.ObjectID, since time.Time) error {
	r := o.repo
	r.leaf.mu.Lock()
	defer r.leaf.mu.Unlock()

	if r.committedAfter(*id, since) || len(o.Holders(id)) > 0 {
		return objectsmod.ErrObjectInUse
	}

	return r.deleteLocked(id)
}

// committedAfter reports whether a commit of id is after t.
func (r *dirTestRepo) committedAfter(id astral.ObjectID, t time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, c := range r.commits {
		if c.ID == id && c.At.After(t) {
			return true
		}
	}

	return false
}

func (o *dirTestObjects) addHolder(h objectsmod.Holder) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.extra = append(o.extra, h)
}

// holderFunc is a holder of the objects a function names.
type holderFunc func(*astral.ObjectID) bool

func (f holderFunc) HoldObject(id *astral.ObjectID) bool { return f(id) }

// setupDirTest gives a test module the fake clock and the test repository.
func setupDirTest(t *testing.T, mod *Module) {
	t.Helper()

	clk := newFakeClock()
	mod.setupDirectories(clk)
	t.Cleanup(mod.cancel)

	objects := &dirTestObjects{repo: newDirTestRepo(mod.db, clk)}
	objects.extra = []objectsmod.Holder{mod}
	mod.Objects = objects
}

// dirRepo returns the test repository of a module.
func dirRepo(t *testing.T, mod *Module) *dirTestRepo {
	t.Helper()

	objects, ok := mod.Objects.(*dirTestObjects)
	if !ok {
		t.Fatalf("objects = %T, want the test objects", mod.Objects)
	}

	return objects.repo
}

// dirClock returns the fake clock of a module.
func dirClock(t *testing.T, mod *Module) *fakeClock {
	t.Helper()

	clk, ok := mod.clock.(*fakeClock)
	if !ok {
		t.Fatalf("clock = %T, want the fake clock", mod.clock)
	}

	return clk
}

// setDirConfig changes the directory config of a module and starts its scheduler over. Call it before any VFS.
func setDirConfig(mod *Module, fn func(*Config)) {
	fn(&mod.config)
	mod.sched = newDirScheduler(mod.config)
}

// isDirectory reports whether bytes begin with the header of a directory.
func isDirectory(b []byte) bool {
	header := append([]byte{byte(len(vfsapi.DirectoryType))}, vfsapi.DirectoryType...)

	return len(b) >= 4+len(header) && bytes.Equal(b[4:4+len(header)], header)
}

// bucketTokens returns the tokens of the bucket of a VFS at the clock's time.
func bucketTokens(mod *Module, id *astral.Identity) float64 {
	s := mod.sched
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.bucket(id.String(), mod.clock.Now())
	b.refill(mod.clock.Now())

	return b.tokens
}

// drainBucket empties the bucket of a VFS.
func drainBucket(mod *Module, id *astral.Identity) {
	s := mod.sched
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bucket(id.String(), mod.clock.Now()).charge(1<<62, mod.clock.Now())
}

// isPending reports whether a VFS is pending.
func isPending(mod *Module, id *astral.Identity) bool {
	mod.sched.mu.Lock()
	defer mod.sched.mu.Unlock()

	return mod.sched.pending[id.String()] != nil
}

// stepAll runs worker steps until one runs nothing, and returns how many ran.
func stepAll(mod *Module) (ran int) {
	for {
		ok, _ := mod.dirWorkerStep()
		if !ok {
			return ran
		}
		ran++
	}
}

// cachedID returns the cached ObjectID of a row, or nil.
func cachedID(t *testing.T, mod *Module, row int64) *astral.ObjectID {
	t.Helper()

	e, err := mod.db.findEntry(row)
	if err != nil {
		t.Fatalf("find row %d: %v", row, err)
	}

	return e.ObjectID
}
