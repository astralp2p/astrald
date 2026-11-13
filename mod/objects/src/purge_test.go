package objects

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

// funcHolder holds the objects its function reports.
// note: a pointer, because the holder set keys holders by value.
type funcHolder struct {
	hold func(objectID *astral.ObjectID) bool
}

func (h *funcHolder) HoldObject(objectID *astral.ObjectID) bool { return h.hold(objectID) }

// holderFunc returns a holder that holds the objects hold reports.
func holderFunc(hold func(objectID *astral.ObjectID) bool) *funcHolder {
	return &funcHolder{hold: hold}
}

// setTimes sets the commit and read times of the row of id in the leaf repo.
func setTimes(t *testing.T, mod *Module, repo string, id *astral.ObjectID, committedAt, readAt int64) {
	t.Helper()

	err := mod.db.Model(&dbTracking{}).
		Where("repository = ? AND object_id = ?", repo, id).
		Updates(map[string]any{"committed_at": committedAt, "read_at": readAt}).Error
	if err != nil {
		t.Fatalf("set times of %v in %v: %v", id, repo, err)
	}
}

// ago returns the time d before now as whole seconds since the Unix epoch.
func ago(d time.Duration) int64 {
	return time.Now().Add(-d).Unix()
}

// purgeIDs purges the repository named repo and returns the answered ObjectIDs, sorted.
func purgeIDs(t *testing.T, mod *Module, repo string, untouched time.Duration, dry bool) []string {
	t.Helper()

	out, errPtr := mod.purgeRepository(astral.NewContext(nil), mod.GetRepository(repo), untouched, dry)
	var ids []string
	for id := range out {
		ids = append(ids, id.String())
	}
	if *errPtr != nil {
		t.Fatalf("purge %v: %v", repo, *errPtr)
	}
	sort.Strings(ids)
	return ids
}

// idStrings returns the strings of ids, sorted.
func idStrings(ids ...*astral.ObjectID) []string {
	var out []string
	for _, id := range ids {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

func expectIDs(t *testing.T, what string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%v: %v; want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%v: %v; want %v", what, got, want)
		}
	}
}

// expectContains fails unless the repository named repo contains id exactly when want is true.
func expectContains(t *testing.T, mod *Module, repo string, id *astral.ObjectID, want bool) {
	t.Helper()

	has, err := mod.GetRepository(repo).Contains(astral.NewContext(nil), id)
	if err != nil {
		t.Fatalf("contains %v in %v: %v", id, repo, err)
	}
	if has != want {
		t.Fatalf("%v contains %v: %v; want %v", repo, id, has, want)
	}
}

// addLocalLeaf registers a memory leaf under name and adds it to the group local.
func addLocalLeaf(t *testing.T, mod *Module, name string) {
	t.Helper()

	if err := mod.AddRepository(name, mem.New(name, 0)); err != nil {
		t.Fatalf("add %v: %v", name, err)
	}
	if err := mod.AddGroup(objects.RepoLocal, name); err != nil {
		t.Fatalf("add %v to local: %v", name, err)
	}
}

// TestPurge_DeletesUnheldUntouched: an unheld object untouched for the window is deleted with its row; a fresh one is kept.
func TestPurge_DeletesUnheldUntouched(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	old := commitData(t, ctx, mod.GetRepository("mem0"), "old")
	fresh := commitData(t, ctx, mod.GetRepository("mem0"), "fresh")
	setTimes(t, mod, "mem0", old, ago(3*time.Hour), ago(3*time.Hour))
	setTimes(t, mod, "mem0", fresh, ago(3*time.Hour), ago(time.Hour))

	got := purgeIDs(t, mod, "mem0", 2*time.Hour, false)

	expectIDs(t, "purged", got, idStrings(old))
	expectContains(t, mod, "mem0", old, false)
	expectContains(t, mod, "mem0", fresh, true)
	expectTracked(t, mod, "mem0", fresh)
}

// TestPurge_HeldKeptInEveryRepository: a held object is kept in every leaf that has it, whatever its times say.
func TestPurge_HeldKeptInEveryRepository(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	addLocalLeaf(t, mod, "disk1")
	addLocalLeaf(t, mod, "disk2")

	held := commitData(t, ctx, mod.GetRepository("disk1"), "held")
	commitData(t, ctx, mod.GetRepository("disk2"), "held")
	setTimes(t, mod, "disk1", held, 1, 0)
	setTimes(t, mod, "disk2", held, 1, 0)
	mod.AddHolder(holderFunc(func(id *astral.ObjectID) bool { return id.IsEqual(held) }))

	got := purgeIDs(t, mod, objects.RepoLocal, 0, false)

	expectIDs(t, "purged", got, nil)
	expectContains(t, mod, "disk1", held, true)
	expectContains(t, mod, "disk2", held, true)
}

// TestPurge_ReadBetweenSelectionAndTurnKeepsObject: a caller's read after the listing and before the candidate's turn keeps it.
func TestPurge_ReadBetweenSelectionAndTurnKeepsObject(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	first := commitData(t, ctx, repo, "first")
	second := commitData(t, ctx, repo, "second")
	setTimes(t, mod, "mem0", first, 1, 0)
	setTimes(t, mod, "mem0", second, 2, 0)

	// why: the holders are asked during the turn of first, after both candidates were listed.
	mod.AddHolder(holderFunc(func(id *astral.ObjectID) bool {
		if id.IsEqual(first) {
			r := openReader(t, ctx, repo, second, 0)
			r.Read(make([]byte, 1))
			r.Close()
		}
		return false
	}))

	got := purgeIDs(t, mod, "mem0", 0, false)

	expectIDs(t, "purged", got, idStrings(first))
	expectContains(t, mod, "mem0", second, true)
}

// TestPurge_CommitBetweenSelectionAndTurnKeepsObject: a commit of a listed candidate before its turn keeps it.
func TestPurge_CommitBetweenSelectionAndTurnKeepsObject(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)
	id := commitData(t, ctx, leaf, "recommitted")
	setTimes(t, mod, "mem0", id, 1, 0)

	run := &purgeRun{mod: mod, cutoff: ago(purgeFloor)}
	rows, _, err := mod.db.listUntouched("mem0", run.cutoff, nil, purgePageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || !rows[0].ObjectID.IsEqual(id) {
		t.Fatalf("candidates %v; want %v", rows, id)
	}

	commitData(t, ctx, leaf, "recommitted")

	purged, err := run.decide(ctx, leaf, id)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if purged {
		t.Fatal("a candidate re-committed before its turn was purged")
	}
	expectContains(t, mod, "mem0", id, true)
}

// TestPurge_CommitAndDeleteWaitForTheLeafLock: a commit and a delete wait while purge holds the leaf's lock.
func TestPurge_CommitAndDeleteWaitForTheLeafLock(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)
	victim := commitData(t, ctx, leaf, "victim")

	w, err := leaf.Create(ctx, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer w.Discard()
	w.Write([]byte("waiting commit"))

	leaf.mu.Lock()
	done := make(chan struct{}, 2)
	go func() { w.Commit(); done <- struct{}{} }()
	go func() { leaf.Delete(ctx, victim); done <- struct{}{} }()

	select {
	case <-done:
		leaf.mu.Unlock()
		t.Fatal("a commit or a delete ran while the leaf's lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	leaf.mu.Unlock()

	for range 2 {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("a commit or a delete did not finish after the lock was released")
		}
	}
}

// TestPurge_ReadDrainedByAConcurrentFlushKeepsObject: a read taken out of the journal by another flush that has not
// written it yet keeps the object.
func TestPurge_ReadDrainedByAConcurrentFlushKeepsObject(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)
	id := commitData(t, ctx, leaf, "read")
	setTimes(t, mod, "mem0", id, 1, 0)

	r := openReader(t, ctx, leaf, id, 0)
	r.Read(make([]byte, 1))
	r.Close()

	j := mod.objectsReadsJournal
	drained := make(chan struct{})
	sink := j.sink
	j.sink = func(batch map[astral.ObjectID]astral.Time) error {
		close(drained)
		time.Sleep(200 * time.Millisecond)
		return sink(batch)
	}
	flushed := make(chan error, 1)
	go func() { flushed <- j.Flush() }()
	<-drained

	run := &purgeRun{mod: mod, cutoff: ago(purgeFloor)}
	purged, err := run.decide(ctx, leaf, id)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if err := <-flushed; err != nil {
		t.Fatalf("flush: %v", err)
	}
	if purged {
		t.Fatal("an object read before the decision was purged while another flush held its read")
	}
	expectContains(t, mod, "mem0", id, true)
}

// TestPurge_DecideWaitsForTheLeafLock: purge decides a candidate under the leaf's lock, so a commit that holds the lock
// finishes before the decision and keeps the object.
func TestPurge_DecideWaitsForTheLeafLock(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)
	id := commitData(t, ctx, leaf, "committed under the lock")
	setTimes(t, mod, "mem0", id, 1, 0)

	run := &purgeRun{mod: mod, cutoff: ago(purgeFloor)}

	leaf.mu.Lock()
	type result struct {
		purged bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		purged, err := run.decide(ctx, leaf, id)
		done <- result{purged, err}
	}()

	select {
	case <-done:
		leaf.mu.Unlock()
		t.Fatal("purge decided a candidate while the leaf's lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	// why: stands for a commit that holds the leaf's lock and writes the commit time.
	setTimes(t, mod, "mem0", id, time.Now().Unix(), 0)
	leaf.mu.Unlock()

	var res result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("purge did not decide after the lock was released")
	}
	if res.err != nil {
		t.Fatalf("decide: %v", res.err)
	}
	if res.purged {
		t.Fatal("a candidate committed while purge waited for the leaf's lock was purged")
	}
	expectContains(t, mod, "mem0", id, true)
}

// TestPurge_ListUntouchedPages: the listing pages through every candidate, least recently touched first, once each.
func TestPurge_ListUntouchedPages(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")

	// why: equal touches across commit and read times make the cursor break ties on object_id.
	times := [][2]int64{{5, 0}, {1, 3}, {3, 0}, {1, 0}, {2, 3}, {3, 1}, {1, 2}}
	var want []string
	for i, tt := range times {
		id := commitData(t, ctx, repo, fmt.Sprintf("row %d", i))
		setTimes(t, mod, "mem0", id, tt[0], tt[1])
		want = append(want, id.String())
	}
	// why: a row touched within the window is not listed.
	commitData(t, ctx, repo, "fresh")

	var got []string
	var lastTouch int64
	var after *untouchedCursor
	for pages := 0; ; pages++ {
		if pages > len(times) {
			t.Fatal("the listing does not end")
		}
		rows, next, err := mod.db.listUntouched("mem0", ago(purgeFloor), after, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			if row.touch() < lastTouch {
				t.Fatalf("touch %v listed after %v", row.touch(), lastTouch)
			}
			lastTouch = row.touch()
			got = append(got, row.ObjectID.String())
		}
		if next == nil {
			break
		}
		after = next
	}

	sort.Strings(got)
	sort.Strings(want)
	expectIDs(t, "listed", got, want)
}

// TestPurge_DryAnswersWhatARunDeletes: dry answers exactly the ObjectIDs a run deletes and deletes nothing.
func TestPurge_DryAnswersWhatARunDeletes(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)

	x := commitData(t, ctx, leaf, "x")
	y := commitData(t, ctx, leaf, "y")
	held := commitData(t, ctx, leaf, "held")
	fresh := commitData(t, ctx, leaf, "fresh")
	missing := commitData(t, ctx, leaf, "missing")
	for _, id := range []*astral.ObjectID{x, y, held, missing} {
		setTimes(t, mod, "mem0", id, 1, 0)
	}
	// why: an object deleted behind the wrapper's back leaves a row without content.
	if err := leaf.inner.Delete(ctx, missing); err != nil {
		t.Fatalf("delete behind the wrapper: %v", err)
	}
	mod.AddHolder(holderFunc(func(id *astral.ObjectID) bool { return id.IsEqual(held) }))

	dry := purgeIDs(t, mod, "mem0", 0, true)

	expectIDs(t, "dry", dry, idStrings(x, y))
	for _, id := range []*astral.ObjectID{x, y, held, fresh} {
		expectContains(t, mod, "mem0", id, true)
	}
	if rows := trackingRows(t, mod, "mem0"); len(rows) != 5 {
		t.Fatalf("rows after a dry purge: %v; want 5", len(rows))
	}

	real := purgeIDs(t, mod, "mem0", 0, false)

	expectIDs(t, "run", real, dry)
	// note: the run drops the row of the missing object and does not answer it.
	if rows := trackingRows(t, mod, "mem0"); len(rows) != 2 {
		t.Fatalf("rows after the run: %v; want the rows of held and fresh", rows)
	}
}

// TestPurge_FloorKeepsRecentCommit: a window below one hour, or none, means one hour.
func TestPurge_FloorKeepsRecentCommit(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")
	recent := commitData(t, ctx, repo, "recent")
	readRecently := commitData(t, ctx, repo, "read recently")
	old := commitData(t, ctx, repo, "old")
	setTimes(t, mod, "mem0", recent, ago(30*time.Minute), 0)
	setTimes(t, mod, "mem0", readRecently, ago(3*time.Hour), ago(30*time.Minute))
	setTimes(t, mod, "mem0", old, ago(2*time.Hour), 0)

	expectIDs(t, "one minute, dry", purgeIDs(t, mod, "mem0", time.Minute, true), idStrings(old))
	expectIDs(t, "untouched omitted", purgeIDs(t, mod, "mem0", 0, false), idStrings(old))
	expectContains(t, mod, "mem0", recent, true)
	expectContains(t, mod, "mem0", readRecently, true)
}

// TestPurge_EmptyObjectIsNeverPurged: a commit of the empty object writes no row, and a row left for it is never
// answered as purged, dry or not.
func TestPurge_EmptyObjectIsNeverPurged(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")

	empty := commitData(t, ctx, repo, "")
	if !objectsmod.IsEmptyObject(empty) {
		t.Fatalf("committed %v; want the empty object", empty)
	}
	if rows := trackingRows(t, mod, "mem0"); len(rows) != 0 {
		t.Fatalf("tracking rows after an empty commit: %v; want none", rows)
	}

	if err := mod.db.trackCommit("mem0", empty, ago(3*time.Hour)); err != nil {
		t.Fatalf("track: %v", err)
	}

	expectIDs(t, "dry", purgeIDs(t, mod, "mem0", 2*time.Hour, true), nil)
	expectIDs(t, "purged", purgeIDs(t, mod, "mem0", 2*time.Hour, false), nil)
}

// TestPurge_GroupVisitsEachLeaf: a group is purged once per leaf below it, nested groups included.
func TestPurge_GroupVisitsEachLeaf(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	addLocalLeaf(t, mod, "disk1")
	addLocalLeaf(t, mod, "disk2")
	// why: a leaf below two groups is purged once.
	mod.AddGroup(objects.RepoRemovable, "disk2")

	var want []*astral.ObjectID
	for _, leaf := range []string{"mem0", objects.RepoSystem, "disk1", "disk2"} {
		id := commitData(t, ctx, mod.GetRepository(leaf), "in "+leaf)
		setTimes(t, mod, leaf, id, 1, 0)
		want = append(want, id)
	}

	leaves := mod.trackedLeaves(mod.GetRepository(objects.RepoDevice))
	if len(leaves) != 4 {
		t.Fatalf("device has %d tracked leaves; want 4", len(leaves))
	}

	got := purgeIDs(t, mod, objects.RepoDevice, 0, false)

	expectIDs(t, "purged", got, idStrings(want...))
}

// TestOpPurge_AnswersPurgedThenEOS: the op takes untouched and dry, answers the purged ObjectIDs, then eos.
func TestOpPurge_AnswersPurgedThenEOS(t *testing.T) {
	mod := newTrackingModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	ctx := astral.NewContext(nil)
	old := commitData(t, ctx, mod.GetRepository("mem0"), "old")
	recent := commitData(t, ctx, mod.GetRepository("mem0"), "recent")
	setTimes(t, mod, "mem0", old, ago(3*time.Hour), 0)
	setTimes(t, mod, "mem0", recent, ago(90*time.Minute), 0)

	answers := routePurge(t, mod, "objects.purge?repo=mem0&untouched=2h&dry=true")

	if len(answers) != 2 {
		t.Fatalf("answered %v; want one ObjectID and eos", answers)
	}
	if id, ok := answers[0].(*astral.ObjectID); !ok || !id.IsEqual(old) {
		t.Fatalf("first answer %v; want %v", answers[0], old)
	}
	if _, ok := answers[1].(*astral.EOS); !ok {
		t.Fatalf("last answer %v; want eos", answers[1])
	}
	expectContains(t, mod, "mem0", old, true)
}

// routePurge routes one objects.purge query and returns every object it answered.
func routePurge(t *testing.T, mod *Module, queryString string) []astral.Object {
	t.Helper()

	w := newRecordingWriter()
	if err := route(t, mod.OpPurge, astral.GenerateIdentity(), queryString, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	rx := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	var answers []astral.Object
	for {
		obj, err := rx.Receive()
		if err == io.EOF {
			return answers
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		answers = append(answers, obj)
	}
}

var _ objectsmod.Holder = &funcHolder{}
