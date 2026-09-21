package objects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// groupTestTimeout bounds every wait in these tests, so a broken group fails instead of hanging.
const groupTestTimeout = 10 * time.Second

// partialTestID is the argument of every lookup in these tests.
var partialTestID = &astral.ObjectID{Hash: [32]byte{0x5a, 0x01}}

// fakeRepo answers Read, Contains and Delete with err, where a nil err is a hit.
// why: no repository in the tree returns ErrHashLookupUnsupported, so the group policy needs a fake that does.
// note: the embedded nil interface panics on any other method, which asserts that a lookup calls nothing else.
type fakeRepo struct {
	objectsmod.Repository
	err error

	// release, when set, holds every answer until the test closes it.
	release chan struct{}

	calls    atomic.Uint64
	lastID   atomic.Pointer[astral.ObjectID]
	reader   atomic.Pointer[fakeReader]
	started  chan struct{}
	starting atomic.Bool
}

func newFakeRepo(err error) *fakeRepo {
	return &fakeRepo{err: err, started: make(chan struct{})}
}

// answer records one call and returns the fake's answer.
func (repo *fakeRepo) answer(objectID *astral.ObjectID) error {
	repo.calls.Add(1)
	repo.lastID.Store(objectID)
	if repo.starting.CompareAndSwap(false, true) {
		close(repo.started)
	}
	if repo.release != nil {
		select {
		case <-repo.release:
		case <-time.After(groupTestTimeout):
		}
	}
	return repo.err
}

// why: the reader exists before the answer, so a test can watch the reader of a held hit.
func (repo *fakeRepo) Read(_ *astral.Context, objectID *astral.ObjectID, _ int64, _ int64) (objectsmod.Reader, error) {
	r := &fakeReader{repo: repo, closed: make(chan struct{})}
	repo.reader.Store(r)
	if err := repo.answer(objectID); err != nil {
		return nil, err
	}
	return r, nil
}

func (repo *fakeRepo) Contains(_ *astral.Context, objectID *astral.ObjectID) (bool, error) {
	err := repo.answer(objectID)
	if errors.Is(err, objectsmod.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (repo *fakeRepo) Delete(_ *astral.Context, objectID *astral.ObjectID) error {
	return repo.answer(objectID)
}

// fakeReader is the reader of a fake hit. It records Close.
type fakeReader struct {
	repo    *fakeRepo
	closed  chan struct{}
	closing atomic.Bool
}

var _ objectsmod.Reader = &fakeReader{}

func (r *fakeReader) Read([]byte) (int, error) { return 0, io.EOF }

func (r *fakeReader) Close() error {
	if r.closing.CompareAndSwap(false, true) {
		close(r.closed)
	}
	return nil
}

func (r *fakeReader) Repo() objectsmod.Repository { return r.repo }

func (r *fakeReader) ID() *astral.ObjectID {
	return &astral.ObjectID{Size: 1, Hash: partialTestID.Hash}
}

// addRepo registers repo in mod under name.
func addRepo(t *testing.T, mod *Module, name string, repo objectsmod.Repository) {
	t.Helper()
	if _, ok := mod.repos.Set(name, repo); !ok {
		t.Fatalf("repository %s already registered", name)
	}
}

// addGroup registers a group of the named members in mod under name.
func addGroup(t *testing.T, mod *Module, name string, concurrent bool, members ...string) *RepoGroup {
	t.Helper()
	group := NewRepoGroup(mod, name, concurrent)
	addRepo(t, mod, name, group)
	for _, member := range members {
		if err := group.Add(member); err != nil {
			t.Fatalf("add %s to %s: %v", member, name, err)
		}
	}
	return group
}

// addFakes registers one fake per answer in mod, named prefix0, prefix1 and so on, and returns the names.
func addFakes(t *testing.T, mod *Module, prefix string, answers []error) []string {
	t.Helper()
	var names []string
	for i, answer := range answers {
		name := fmt.Sprintf("%s%d", prefix, i)
		addRepo(t, mod, name, newFakeRepo(answer))
		names = append(names, name)
	}
	return names
}

// fake returns the fake registered in group's module under name.
func fake(t *testing.T, group *RepoGroup, name string) *fakeRepo {
	t.Helper()
	repo, ok := group.mod.GetRepository(name).(*fakeRepo)
	if !ok {
		t.Fatalf("%s is not a fake repository", name)
	}
	return repo
}

// waitClosed fails the test when ch is not closed in time.
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(groupTestTimeout):
		t.Fatalf("%s: timed out", what)
	}
}

// waitErr returns the error sent on result, and fails the test when none arrives in time.
func waitErr(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(groupTestTimeout):
		t.Fatal("the lookup did not return")
	}
	return nil
}

// groupOutcome is what a group lookup answers.
type groupOutcome int

const (
	outcomeHit groupOutcome = iota
	outcomeMiss
	outcomeUnsupported
)

func (outcome groupOutcome) String() string {
	return [...]string{"hit", "miss", "unsupported"}[outcome]
}

// groupLookup runs one group method and returns its outcome and, for a Read hit, the returned reader.
// cancellable runs the same method under a caller's context and returns only its error.
type groupLookup struct {
	name        string
	concurrent  bool
	run         func(t *testing.T, group *RepoGroup) (groupOutcome, *fakeReader)
	cancellable func(ctx *astral.Context, group *RepoGroup) error
}

var groupLookups = []groupLookup{
	{name: "read sequential", concurrent: false, run: runRead, cancellable: readCancellable},
	{name: "read concurrent", concurrent: true, run: runRead, cancellable: readCancellable},
	{name: "contains", run: runContains, cancellable: containsCancellable},
	{name: "delete", run: runDelete, cancellable: deleteCancellable},
}

func runRead(t *testing.T, group *RepoGroup) (groupOutcome, *fakeReader) {
	t.Helper()
	r, err := group.Read(astral.NewContext(nil), partialTestID, 0, 0)
	switch {
	case err == nil:
		reader, ok := r.(*fakeReader)
		if !ok {
			t.Fatalf("Read returned %T; want the member's reader unchanged", r)
		}
		return outcomeHit, reader
	case errors.Is(err, objectsmod.ErrHashLookupUnsupported):
		return outcomeUnsupported, nil
	case errors.Is(err, objectsmod.ErrNotFound):
		return outcomeMiss, nil
	}
	t.Fatalf("Read: unexpected error %v", err)
	return 0, nil
}

func runContains(t *testing.T, group *RepoGroup) (groupOutcome, *fakeReader) {
	t.Helper()
	has, err := group.Contains(astral.NewContext(nil), partialTestID)
	switch {
	case err == nil && has:
		return outcomeHit, nil
	case err == nil:
		return outcomeMiss, nil
	case errors.Is(err, objectsmod.ErrHashLookupUnsupported) && !has:
		return outcomeUnsupported, nil
	}
	t.Fatalf("Contains: unexpected answer %v, %v", has, err)
	return 0, nil
}

func runDelete(t *testing.T, group *RepoGroup) (groupOutcome, *fakeReader) {
	t.Helper()
	err := group.Delete(astral.NewContext(nil), partialTestID)
	switch {
	case err == nil:
		return outcomeHit, nil
	case errors.Is(err, objectsmod.ErrHashLookupUnsupported):
		return outcomeUnsupported, nil
	case errors.Is(err, objectsmod.ErrNotFound):
		return outcomeMiss, nil
	}
	t.Fatalf("Delete: unexpected error %v", err)
	return 0, nil
}

func readCancellable(ctx *astral.Context, group *RepoGroup) error {
	_, err := group.Read(ctx, partialTestID, 0, 0)
	return err
}

func containsCancellable(ctx *astral.Context, group *RepoGroup) error {
	_, err := group.Contains(ctx, partialTestID)
	return err
}

func deleteCancellable(ctx *astral.Context, group *RepoGroup) error {
	return group.Delete(ctx, partialTestID)
}

// policyCase is one set of member answers and the group outcome it must produce.
type policyCase struct {
	name string
	// answers holds one fake member per entry, in order; a nil entry is a hit.
	answers []error
	// missing puts a member name with no registered repository first.
	missing bool
	want    groupOutcome
}

var (
	errUnsupported = objectsmod.ErrHashLookupUnsupported
	errNotFound    = objectsmod.ErrNotFound
	errAmbiguous   = objectsmod.ErrAmbiguousObjectID
	errDisk        = errors.New("disk failure")
)

var policyCases = []policyCase{
	{name: "all unsupported", answers: []error{errUnsupported, errUnsupported}, want: outcomeUnsupported},
	{name: "one unsupported", answers: []error{errUnsupported}, want: outcomeUnsupported},
	{name: "wrapped unsupported", answers: []error{fmt.Errorf("fs: %w", errUnsupported), errUnsupported}, want: outcomeUnsupported},
	{name: "miss then unsupported", answers: []error{errNotFound, errUnsupported}, want: outcomeMiss},
	{name: "unsupported then miss", answers: []error{errUnsupported, errNotFound}, want: outcomeMiss},
	{name: "unsupported then hit", answers: []error{errUnsupported, nil}, want: outcomeHit},
	{name: "hit then unsupported", answers: []error{nil, errUnsupported}, want: outcomeHit},
	{name: "empty group", want: outcomeMiss},
	{name: "missing member alone", missing: true, want: outcomeMiss},
	{name: "missing member and unsupported", answers: []error{errUnsupported}, missing: true, want: outcomeUnsupported},
	{name: "ambiguous alone", answers: []error{errAmbiguous}, want: outcomeMiss},
	{name: "ambiguous and unsupported", answers: []error{errAmbiguous, errUnsupported}, want: outcomeMiss},
	{name: "ambiguous then hit", answers: []error{errAmbiguous, nil}, want: outcomeHit},
	{name: "other error and unsupported", answers: []error{errDisk, errUnsupported}, want: outcomeMiss},
	{name: "broad ErrUnsupported is not the sentinel", answers: []error{errors.ErrUnsupported}, want: outcomeMiss},
	{name: "member's own timeout then hit", answers: []error{context.DeadlineExceeded, nil}, want: outcomeHit},
	{name: "member's own timeout alone", answers: []error{context.DeadlineExceeded}, want: outcomeMiss},
}

// newFlatGroup registers a group whose members are one fake per answer, named r0, r1 and so on.
// With missing, a member name with no registered repository comes first.
func newFlatGroup(t *testing.T, concurrent bool, missing bool, answers []error) *RepoGroup {
	t.Helper()
	mod := &Module{}
	group := addGroup(t, mod, "group", concurrent)
	if missing {
		if err := group.repos.Add("gone"); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range addFakes(t, mod, "r", answers) {
		if err := group.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	return group
}

func TestRepoGroup_UnsupportedLookupPolicy(t *testing.T) {
	for _, c := range policyCases {
		for _, lookup := range groupLookups {
			t.Run(c.name+"/"+lookup.name, func(t *testing.T) {
				checkPolicyCase(t, c, lookup)
			})
		}
	}
}

func checkPolicyCase(t *testing.T, c policyCase, lookup groupLookup) {
	group := newFlatGroup(t, lookup.concurrent, c.missing, c.answers)

	got, reader := lookup.run(t, group)
	if got != c.want {
		t.Fatalf("outcome %v; want %v", got, c.want)
	}

	if reader != nil && (reader.repo.err != nil || reader.repo.reader.Load() != reader) {
		t.Error("Read returned a reader that is not a hit's reader")
	}

	for i := range c.answers {
		id := fake(t, group, fmt.Sprintf("r%d", i)).lastID.Load()
		if id != nil && id != partialTestID {
			t.Errorf("r%d received %v; want the group's argument", i, id)
		}
	}
}

func TestRepoGroup_DeleteFansOutToEveryMember(t *testing.T) {
	answers := []error{nil, errUnsupported, nil, errNotFound}
	group := newFlatGroup(t, false, false, answers)

	if err := group.Delete(astral.NewContext(nil), partialTestID); err != nil {
		t.Fatalf("Delete: %v; want nil after one deletion", err)
	}
	for i := range answers {
		if n := fake(t, group, fmt.Sprintf("r%d", i)).calls.Load(); n != 1 {
			t.Errorf("r%d: %d calls; want 1", i, n)
		}
	}
}

// TestRepoGroup_DeleteCancelledMidFanOut: the context ends while r1 answers, so r2 is never tried.
// A deletion by r0 before that still makes Delete succeed; without one, Delete answers the context error.
func TestRepoGroup_DeleteCancelledMidFanOut(t *testing.T) {
	cases := []struct {
		name  string
		first error
		want  error
	}{
		{name: "deleted before cancellation", first: nil, want: nil},
		{name: "nothing deleted before cancellation", first: errNotFound, want: context.Canceled},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			group := newFlatGroup(t, false, false, []error{c.first, errNotFound, nil})
			held := fake(t, group, "r1")
			release := make(chan struct{})
			held.release = release

			ctx, cancel := astral.NewContext(nil).WithCancel()
			defer cancel()

			result := make(chan error, 1)
			go func() {
				result <- deleteCancellable(ctx, group)
			}()

			waitClosed(t, held.started, "held member")
			cancel()
			close(release)

			if err := waitErr(t, result); !errors.Is(err, c.want) {
				t.Fatalf("Delete: %v; want %v", err, c.want)
			}
			if n := fake(t, group, "r2").calls.Load(); n != 0 {
				t.Errorf("r2 invoked %d times after cancellation; want 0", n)
			}
		})
	}
}

// newNestedGroups registers the default topology main -> device -> local with fake leaves.
// main is sequential over device and virtual, device is sequential over local, local and virtual are concurrent.
// The leaves of local are local0, local1 and so on; the leaves of virtual are virtual0, virtual1 and so on.
func newNestedGroups(t *testing.T, local []error, virtual []error) *RepoGroup {
	t.Helper()
	mod := &Module{}
	addGroup(t, mod, "local", true, addFakes(t, mod, "local", local)...)
	addGroup(t, mod, "device", false, "local")
	addGroup(t, mod, "virtual", true, addFakes(t, mod, "virtual", virtual)...)
	return addGroup(t, mod, "main", false, "device", "virtual")
}

func TestRepoGroup_NestedGroupsPropagateTheSentinel(t *testing.T) {
	cases := []struct {
		name           string
		local, virtual []error
		want           groupOutcome
	}{
		{name: "every leaf unsupported", local: []error{errUnsupported, errUnsupported}, virtual: []error{errUnsupported}, want: outcomeUnsupported},
		{name: "empty inner group misses beside unsupported", virtual: []error{errUnsupported}, want: outcomeMiss},
		{name: "one leaf misses", local: []error{errUnsupported, errNotFound}, virtual: []error{errUnsupported}, want: outcomeMiss},
		{name: "one leaf is ambiguous", local: []error{errUnsupported}, virtual: []error{errAmbiguous}, want: outcomeMiss},
		{name: "one leaf hits", local: []error{errUnsupported}, virtual: []error{errUnsupported, nil}, want: outcomeHit},
	}

	for _, c := range cases {
		for _, lookup := range groupLookups {
			if lookup.concurrent {
				continue
			}
			t.Run(c.name+"/"+lookup.name, func(t *testing.T) {
				main := newNestedGroups(t, c.local, c.virtual)
				if got, _ := lookup.run(t, main); got != c.want {
					t.Fatalf("outcome %v; want %v", got, c.want)
				}
			})
		}
	}
}

func TestRepoGroup_CancelledContextReachesNoMember(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprintf("concurrent=%v", concurrent), func(t *testing.T) {
			group := newFlatGroup(t, concurrent, false, []error{nil})

			ctx, cancel := astral.NewContext(nil).WithCancel()
			cancel()

			if _, err := group.Read(ctx, partialTestID, 0, 0); !errors.Is(err, context.Canceled) {
				t.Errorf("Read: %v; want context.Canceled", err)
			}
			if has, err := group.Contains(ctx, partialTestID); has || !errors.Is(err, context.Canceled) {
				t.Errorf("Contains: %v, %v; want false, context.Canceled", has, err)
			}
			if err := group.Delete(ctx, partialTestID); !errors.Is(err, context.Canceled) {
				t.Errorf("Delete: %v; want context.Canceled", err)
			}
			if n := fake(t, group, "r0").calls.Load(); n != 0 {
				t.Errorf("member invoked %d times under a cancelled context; want 0", n)
			}
		})
	}
}

func TestRepoGroup_SequentialReadPropagatesMemberContextError(t *testing.T) {
	group := newFlatGroup(t, false, false, []error{context.Canceled, nil})
	held := fake(t, group, "r0")
	release := make(chan struct{})
	held.release = release

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- readCancellable(ctx, group)
	}()

	waitClosed(t, held.started, "held member")
	cancel()
	close(release)

	if err := waitErr(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read: %v; want context.Canceled", err)
	}
	if n := fake(t, group, "r1").calls.Load(); n != 0 {
		t.Errorf("the member after the cancelled one was invoked %d times; want 0", n)
	}
}

func TestRepoGroup_NestedCancellationReturnsTheContextError(t *testing.T) {
	for _, lookup := range groupLookups {
		if lookup.concurrent {
			continue
		}
		t.Run(lookup.name, func(t *testing.T) {
			main := newNestedGroups(t, []error{context.Canceled}, []error{nil})
			held := fake(t, main, "local0")
			release := make(chan struct{})
			held.release = release

			ctx, cancel := astral.NewContext(nil).WithCancel()
			defer cancel()

			result := make(chan error, 1)
			go func() {
				result <- lookup.cancellable(ctx, main)
			}()

			waitClosed(t, held.started, "local member")
			cancel()
			close(release)

			if err := waitErr(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("%v; want context.Canceled from the nested groups", err)
			}
			if n := fake(t, main, "virtual0").calls.Load(); n != 0 {
				t.Errorf("virtual member invoked %d times after cancellation; want 0", n)
			}
		})
	}
}

func TestRepoGroup_ConcurrentReadClosesLosingReader(t *testing.T) {
	group := newFlatGroup(t, true, false, []error{nil, nil})
	slow, fast := fake(t, group, "r0"), fake(t, group, "r1")
	release := make(chan struct{})
	slow.release = release

	r, err := group.Read(astral.NewContext(nil), partialTestID, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if r != fast.reader.Load() {
		t.Fatalf("Read returned %v; want the fast member's reader", r)
	}

	waitClosed(t, slow.started, "slow member")
	close(release)
	waitClosed(t, slow.reader.Load().closed, "Close of the losing reader")

	if fast.reader.Load().closing.Load() {
		t.Error("the returned reader was closed")
	}
}

func TestRepoGroup_ConcurrentReadCancelled(t *testing.T) {
	group := newFlatGroup(t, true, false, []error{nil, context.Canceled})
	heldHit, heldCancelled := fake(t, group, "r0"), fake(t, group, "r1")
	release := make(chan struct{})
	heldHit.release, heldCancelled.release = release, release

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- readCancellable(ctx, group)
	}()

	waitClosed(t, heldHit.started, "held hit")
	waitClosed(t, heldCancelled.started, "held cancelled member")
	cancel()

	if err := waitErr(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read: %v; want context.Canceled", err)
	}

	// why: the hit arrives after the group returned, so only the collector's drain can close its reader.
	close(release)
	waitClosed(t, heldHit.reader.Load().closed, "Close of a hit after cancellation")
}
