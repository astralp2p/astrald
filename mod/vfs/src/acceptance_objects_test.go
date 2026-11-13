package vfs_test

import (
	"bytes"
	"net/http"
	"slices"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// commit writes content to a repository of the node, as a module of the node does, and returns its ObjectID.
func (a *acceptanceNode) commit(repo string, content []byte) *astral.ObjectID {
	a.t.Helper()

	w, err := a.objects().GetRepository(repo).Create(a.ctx, nil)
	if err != nil {
		a.t.Fatalf("create in %v: %v", repo, err)
	}
	defer w.Discard()

	if _, err = w.Write(content); err != nil {
		a.t.Fatalf("write to %v: %v", repo, err)
	}
	id, err := w.Commit()
	if err != nil {
		a.t.Fatalf("commit to %v: %v", repo, err)
	}

	return id
}

// indexed returns the ObjectIDs in a column of an index table of the node.
func (a *acceptanceNode) indexed(table, column string) []string {
	a.t.Helper()

	var ids []string
	if err := a.db.Table(table).Pluck(column, &ids).Error; err != nil {
		a.t.Fatalf("%v of %v: %v", column, table, err)
	}
	slices.Sort(ids)

	return ids
}

// expectPurged fails unless a purge answered exactly the ObjectIDs want.
func expectPurged(t *testing.T, what string, got []string, want ...*astral.ObjectID) {
	t.Helper()

	var ids []string
	for _, id := range want {
		ids = append(ids, id.String())
	}
	slices.Sort(ids)
	slices.Sort(got)
	if !slices.Equal(got, ids) {
		t.Errorf("%v purged %v; want %v", what, got, ids)
	}
}

// TestAcceptancePurgeWindow (acceptance criteria 8 and 12): objects.purge of local with untouched deletes exactly
// the objects that nothing holds and that were neither committed nor read within the window; a file's content and
// the objects other holders hold are kept; a dry purge answers the same ObjectIDs and deletes nothing; with
// untouched left out, the one-hour floor keeps an object committed half an hour ago.
func TestAcceptancePurgeWindow(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()

	file := a.mustWrite(app.client, "/kept.txt", []byte("the content of a file"))
	untouched := a.commit(objectsapi.RepoLocal, []byte("untouched"))
	read := a.commit(objectsapi.RepoLocal, []byte("read within the window"))
	a.age(3 * time.Hour)

	if _, err := a.readRaw(a.as(a.nodeID()), "objects.read", query.Args{"id": read}); err != nil {
		t.Fatalf("objects.read: %v", err)
	}
	fresh := a.commit(objectsapi.RepoLocal, []byte("committed within the window"))

	dry := a.purge(objectsapi.RepoLocal, 2*time.Hour, true)
	expectPurged(t, "a dry purge", dry, untouched)
	if !a.contains(objectsapi.RepoLocal, untouched) {
		t.Fatal("a dry purge deleted an object")
	}

	run := a.purge(objectsapi.RepoLocal, 2*time.Hour, false)
	expectPurged(t, "a purge", run, untouched)
	if !slices.Equal(dry, run) {
		t.Errorf("the dry purge answered %v and the purge %v", dry, run)
	}
	if a.contains(objectsapi.RepoLocal, untouched) {
		t.Error("the purged object is still in local")
	}
	for name, id := range map[string]*astral.ObjectID{"file": &file.ID, "read": read, "fresh": fresh} {
		if !a.contains(objectsapi.RepoLocal, id) {
			t.Errorf("the %v object was deleted", name)
		}
	}

	old := a.commit(objectsapi.RepoLocal, []byte("committed two hours ago"))
	a.age(90 * time.Minute)
	recent := a.commit(objectsapi.RepoLocal, []byte("committed half an hour ago"))
	a.age(30 * time.Minute)

	expectPurged(t, "a purge without untouched", a.purge(objectsapi.RepoLocal, 0, false), old, read, fresh)
	if !a.contains(objectsapi.RepoLocal, recent) || !a.contains(objectsapi.RepoLocal, &file.ID) {
		t.Error("a purge without untouched deleted an object touched within one hour, or a file's content")
	}
}

// TestAcceptanceHoldersSurviveAPurgeOfLocal (acceptance criterion 13): an indexed key and an active contract are
// still in local after a purge of local with a short window, while an unheld object is purged.
func TestAcceptanceHoldersSurviveAPurgeOfLocal(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	a.registerApp()

	unheld := a.commit(objectsapi.RepoLocal, []byte("unheld"))
	keys := a.inLocal(a.indexed("crypto__private_keys", "key_id"))
	contracts := a.inLocal(a.indexed("auth__contracts", "object_id"))
	if len(keys) == 0 || len(contracts) == 0 {
		t.Fatalf("indexed keys %v and contracts %v in local; want some of each", keys, contracts)
	}

	a.age(3 * time.Hour)
	expectPurged(t, "a purge of local", a.purge(objectsapi.RepoLocal, time.Hour, false), unheld)

	for _, id := range append(keys, contracts...) {
		if !a.contains(objectsapi.RepoLocal, id) {
			t.Errorf("%v, an indexed key or an active contract, is not in local after the purge", id)
		}
	}
}

// inLocal returns the ObjectIDs among ids that local has.
// note: the node key is indexed too, and it is stored in the repository system, not in local.
func (a *acceptanceNode) inLocal(ids []string) []*astral.ObjectID {
	a.t.Helper()

	var local []*astral.ObjectID
	for _, s := range ids {
		id, err := astral.ParseID(s)
		if err != nil {
			a.t.Fatalf("parse %v: %v", s, err)
		}
		if a.contains(objectsapi.RepoLocal, id) {
			local = append(local, id)
		}
	}

	return local
}

// TestAcceptanceReadRoutesMoveTheReadTime (acceptance criterion 9): a read through objects.read, objects.load,
// vfs.read, the HTTP object handler, and a group read served from a memory copy while a disk copy exists moves the
// read time of every tracked copy.
func TestAcceptanceReadRoutesMoveTheReadTime(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()
	content := []byte("read through every route")
	entry := a.mustWrite(app.client, "/read.txt", content)
	id := &entry.ID

	a.expectReadMoves("objects.read", id, 1, func() error {
		_, err := a.readRaw(app.client, "objects.read", query.Args{"id": id})
		return err
	})
	a.expectReadMoves("objects.load", id, 1, func() error {
		_, err := a.call(app.client, "objects.load", query.Args{"id": id, "unparsed": true})
		return err
	})
	a.expectReadMoves("vfs.read", id, 1, func() error {
		_, err := a.readRaw(app.client, "vfs.read", query.Args{"path": "/read.txt"})
		return err
	})
	a.expectReadMoves("the HTTP object handler", id, 1, func() error {
		if status, body := a.httpGet(app.token, "/.objects/"+id.String()); status != http.StatusOK || !bytes.Equal(body, content) {
			t.Errorf("GET /.objects/%v = %v, %q", id, status, body)
		}
		return nil
	})

	if mem := a.commit("mem0", content); !mem.IsEqual(id) {
		t.Fatalf("the memory copy is %v; want %v", mem, id)
	}
	probe, err := a.objects().Probe(a.ctx, a.objects().GetRepository(objectsapi.RepoDevice), id)
	if err != nil || probe.Repo != "mem0" {
		t.Fatalf("a read of the group device is served by %v, %v; want mem0", probe, err)
	}
	a.expectReadMoves("a group read served from memory", id, 2, func() error {
		_, err := a.readRaw(app.client, "objects.read", query.Args{"id": id, "repo": objectsapi.RepoDevice})
		return err
	})
}

// expectReadMoves moves every read time two hours back, reads the object through one route, writes the reads
// journal, and fails unless the object has the given number of tracked copies, each with a read time at or after
// the read.
func (a *acceptanceNode) expectReadMoves(route string, id *astral.ObjectID, copies int, read func() error) {
	a.t.Helper()

	a.age(2 * time.Hour)
	start := time.Now().Unix()
	if err := read(); err != nil {
		a.t.Fatalf("%v: %v", route, err)
	}
	a.flushReads()

	rows := a.tracking(id)
	if len(rows) != copies {
		a.t.Fatalf("%v: tracked copies %v; want %v", route, rows, copies)
	}
	for leaf, row := range rows {
		if row.ReadAt < start {
			a.t.Errorf("%v: the read time of the copy in %v is %v; want at least %v", route, leaf, row.ReadAt, start)
		}
	}
}

// TestAcceptanceHousekeepingLeavesReadTimes (acceptance criterion 10): a run of the auth follower, a run of the
// crypto follower and a Probe leave every read time unchanged.
//
// note: the node restarts with the contract and key indexes emptied, so each follower loads every contract and
// key in local again; the indexes filling up again show the runs happened.
// note: the read times are first moved two hours back, so a counted read within the test shows as a change.
func TestAcceptanceHousekeepingLeavesReadTimes(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	a.registerApp()
	a.commit(objectsapi.RepoLocal, []byte("a plain object"))
	a.commit(objectsapi.RepoLocal, bytes.Repeat([]byte{1}, 70<<10))

	contracts := a.indexed("auth__contracts", "object_id")
	keys := a.indexed("crypto__private_keys", "key_id")
	a.flushReads()
	a.age(2 * time.Hour)
	before := a.trackingLeaf(a.localLeaf())

	a.stop()
	for _, table := range []string{"auth__contract_permits", "auth__contracts", "crypto__private_keys"} {
		if err := a.db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("empty %v: %v", table, err)
		}
	}
	a.run()

	a.eventually("the followers index every contract and key again", func() bool {
		return slices.Equal(a.indexed("auth__contracts", "object_id"), contracts) &&
			slices.Equal(a.indexed("crypto__private_keys", "key_id"), keys)
	})
	a.flushReads()
	// note: the crypto module stores the node key in system again at every start and signs with it, so only the
	// rows of local are compared across the restart.
	expectSameRows(t, "after a run of each follower", a.trackingLeaf(a.localLeaf()), before)

	a.age(2 * time.Hour)
	before = a.trackingLeaf("")
	for id := range before {
		answer, err := a.call(a.as(a.nodeID()), "objects.probe", query.Args{"id": id, "repo": objectsapi.RepoDevice})
		if _, ok := answer.(*objectsapi.Probe); err != nil || !ok {
			t.Errorf("objects.probe %v answered %#v, %v", id, answer, err)
		}
	}
	a.flushReads()
	expectSameRows(t, "after a probe of every object", a.trackingLeaf(""), before)
}

// trackingLeaf returns the tracking rows of one leaf, or of every leaf when leaf is empty, by ObjectID and leaf.
func (a *acceptanceNode) trackingLeaf(leaf string) map[string]acceptanceTracking {
	a.t.Helper()

	type row struct {
		Repository  string
		ObjectID    string
		CommittedAt int64
		ReadAt      int64
	}
	var rows []row
	q := a.db.Table("objects__tracking")
	if leaf != "" {
		q = q.Where("repository = ?", leaf)
	}
	if err := q.Find(&rows).Error; err != nil {
		a.t.Fatalf("tracking rows: %v", err)
	}

	byID := map[string]acceptanceTracking{}
	for _, r := range rows {
		byID[r.ObjectID] = acceptanceTracking{Repository: r.Repository, CommittedAt: r.CommittedAt, ReadAt: r.ReadAt}
	}

	return byID
}

// expectSameRows fails unless the tracking rows are those before, with the same times.
func expectSameRows(t *testing.T, when string, got, before map[string]acceptanceTracking) {
	t.Helper()

	if len(before) == 0 || len(got) != len(before) {
		t.Fatalf("%v: %v tracking rows; want the %v from before", when, len(got), len(before))
	}
	for _, row := range before {
		if row.CommittedAt == 0 {
			t.Fatalf("%v: a row from before has no commit time: %+v", when, row)
		}
	}
	for id, row := range before {
		if got[id] != row {
			t.Errorf("%v: %v is %+v; want %+v", when, id, got[id], row)
		}
	}
}
