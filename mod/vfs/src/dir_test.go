package vfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// The tests of storing directories: acceptance criteria of the ticket "VFS: store folder listings as objects" in
// its order, then the invariants of the write-back. Every timing runs on the fake clock.

// dirVFS is one VFS of a test module, for the test that changes it.
type dirVFS struct {
	t   *testing.T
	mod *Module
	vol *dbVolume
}

// inVFS returns the VFS vol of mod for the test t.
func inVFS(t *testing.T, mod *Module, vol *dbVolume) dirVFS { return dirVFS{t: t, mod: mod, vol: vol} }

// write records a file at a path in one change, as vfs.write does after its commit.
func (v dirVFS) write(path, content string) {
	v.t.Helper()

	names, err := parsePath(path)
	if err != nil {
		v.t.Fatal(err)
	}
	if _, err = v.mod.recordFile(v.vol, names, contentID(content)); err != nil {
		v.t.Fatalf("write %v: %v", path, err)
	}
}

// dirRemove removes the entry at a path in one change.
func dirRemove(t *testing.T, mod *Module, vol *dbVolume, path string) {
	t.Helper()

	names, err := parsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = mod.changeVFS(vol, func(c *change) error { return c.removeEntry(names, true) }); err != nil {
		t.Fatalf("remove %v: %v", path, err)
	}
}

// dirStat answers vfs.stat of a path as the VFS identity.
func dirStat(t *testing.T, mod *Module, vol *dbVolume, path string) *vfsapi.Entry {
	t.Helper()

	return changeTestEntry(t, changeTestCall(t, mod.OpStat, vol, map[string]any{"path": path}))
}

// freshRootID computes the root ObjectID of a VFS from its rows alone, ignoring every cached folder ID.
func freshRootID(t *testing.T, mod *Module, vol *dbVolume) astral.ObjectID {
	t.Helper()

	var id astral.ObjectID
	errRollback := errors.New("rollback")

	err := mod.db.inTx(func(tx *DB) error {
		if err := tx.Exec(`UPDATE vfs__entries SET object_id = NULL WHERE vfs = ? AND flags & ? != 0`,
			vol.Identity, uint16(vfsapi.FlagFolder)).Error; err != nil {
			return err
		}
		root, err := tx.findEntry(vol.RootID)
		if err != nil {
			return err
		}
		plan, err := planFolder(tx, vol.Identity, root, 0)
		if err != nil {
			return err
		}
		id = plan.ID

		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("fresh root: %v", err)
	}

	return id
}

// requireStoredRoot checks that the root is cached, equals the root computed from the rows, and is stored.
func requireStoredRoot(t *testing.T, mod *Module, vol *dbVolume) astral.ObjectID {
	t.Helper()

	cached := cachedID(t, mod, vol.RootID)
	if cached == nil {
		t.Fatal("the root is not cached")
	}
	if want := freshRootID(t, mod, vol); *cached != want {
		t.Fatalf("cached root %v, want %v from the rows", cached, want)
	}
	if !dirRepo(t, mod).has(*cached) {
		t.Fatal("the cached root is not stored")
	}

	return *cached
}

// AC 1: the ObjectID vfs.stat answers names a stored directory with those bytes.
func TestStatFolderIDIsStored(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a.txt", "hello")

	entry := dirStat(t, mod, vol, "/docs")

	commits := dirRepo(t, mod).directoryCommits()
	var found bool
	for _, c := range commits {
		sum := sha256.Sum256(c.Bytes)
		if c.ID == entry.ID {
			found = sum == entry.ID.Hash && uint64(len(c.Bytes)) == entry.ID.Size && isDirectory(c.Bytes)
		}
	}
	if !found {
		t.Fatalf("no stored directory with ObjectID %v in %d commits", entry.ID, len(commits))
	}
	if got := cachedID(t, mod, changeTestMustFind(t, mod, vol, "/docs").ID); got == nil || *got != entry.ID {
		t.Fatalf("cached %v, want %v", got, entry.ID)
	}
}

// AC 2: the stored directory of the empty folder is vector V1 of the type in force.
func TestEmptyFolderDirectoryMatchesVectorV1(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	info, err := mod.vfsInfo(vol)
	if err != nil {
		t.Fatalf("vfsInfo: %v", err)
	}
	if info.Root.Size != v1Size || hex.EncodeToString(info.Root.Hash[:]) != v1Hash {
		t.Fatalf("root %v, want V1", info.Root)
	}

	commits := dirRepo(t, mod).directoryCommits()
	if len(commits) != 1 || commits[0].ID != info.Root {
		t.Fatalf("commits %v, want V1 once", commits)
	}
	if got := hex.EncodeToString(commits[0].Bytes); got != "41444330116d6f642e7666732e6469726563746f727900000000" {
		t.Fatalf("bytes %v, want V1", got)
	}
}

// AC 3: writes less than the quiet time apart, for less than the longest wait, store no directory.
func TestBurstOfWritesStoresNothing(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)

	for i := range 100 {
		inVFS(t, mod, vol).write("/burst/f"+string(rune('a'+i%26))+string(rune('a'+i/26)), "x")
		clk.Advance(490 * time.Millisecond)
		stepAll(mod)
	}

	if n := len(dirRepo(t, mod).directoryCommits()); n != 0 {
		t.Fatalf("%d directories stored during the burst, want 0", n)
	}
}

// AC 4: the quiet time after the last change stores every changed folder, and vfs.statfs then stores nothing.
func TestQuietStoresEveryChangedFolder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)

	inVFS(t, mod, vol).write("/a/b/f", "1")
	inVFS(t, mod, vol).write("/c/g", "2")

	clk.Advance(4 * time.Second)
	if stepAll(mod) != 0 {
		t.Fatal("a compute ran before the quiet time")
	}
	clk.Advance(time.Second)
	if stepAll(mod) != 1 {
		t.Fatal("no compute after the quiet time")
	}

	requireStoredRoot(t, mod, vol)
	for _, path := range []string{"/a", "/a/b", "/c"} {
		id := cachedID(t, mod, changeTestMustFind(t, mod, vol, path).ID)
		if id == nil || !repo.has(*id) {
			t.Fatalf("%v: cached %v, not stored", path, id)
		}
	}

	before := len(repo.directoryCommits())
	if _, err := mod.vfsInfo(vol); err != nil {
		t.Fatal(err)
	}
	if after := len(repo.directoryCommits()); after != before {
		t.Fatalf("vfs.statfs stored %d directories, want 0", after-before)
	}
}

// AC 5: a VFS changed every second gets its directories stored about once every longest wait.
func TestContinuousChangesStoreAboutEveryMinute(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)

	var runs []time.Time
	for i := range 300 {
		inVFS(t, mod, vol).write("/f", string(rune('a'+i%26)))
		clk.Advance(time.Second)
		if stepAll(mod) > 0 {
			runs = append(runs, clk.Now())
		}
	}

	if len(runs) < 4 || len(runs) > 5 {
		t.Fatalf("%d computes in 300 s, want 4 or 5", len(runs))
	}
	for i := 1; i < len(runs); i++ {
		if gap := runs[i].Sub(runs[i-1]); gap < 59*time.Second || gap > 62*time.Second {
			t.Fatalf("computes %v apart, want about 60 s", gap)
		}
	}
}

// AC 6: a VFS whose bucket cannot pay waits until the bucket refilled at the refill rate.
func TestEmptyBucketDefersUntilRefilled(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)

	// nine empty folders and the root: an estimate of ten minimum charges, 40 KiB, or 10 s of refill
	for i := range 9 {
		names := []string{string(rune('a' + i))}
		if err := mod.changeVFS(vol, func(c *change) error { return c.createFolder(names) }); err != nil {
			t.Fatal(err)
		}
	}
	drainBucket(mod, vol.Identity)
	start := clk.Now()

	for clk.Now().Sub(start) < 9*time.Second {
		clk.Advance(time.Second)
		if stepAll(mod) != 0 {
			t.Fatalf("computed after %v, before the bucket refilled", clk.Now().Sub(start))
		}
	}
	clk.Advance(time.Second)
	if stepAll(mod) != 1 {
		t.Fatal("not computed once the bucket refilled")
	}
	requireStoredRoot(t, mod, vol)
}

// AC 6: a VFS with small folders changed once per minute is computed after every change.
func TestSmallFoldersOncePerMinuteComputedEachTime(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)

	for i := range 5 {
		inVFS(t, mod, vol).write("/docs/f", string(rune('a'+i)))
		clk.Advance(5 * time.Second)
		if n := stepAll(mod); n != 1 {
			t.Fatalf("change %d: %d computes, want 1", i, n)
		}
		requireStoredRoot(t, mod, vol)
		clk.Advance(55 * time.Second)
		if n := stepAll(mod); n != 0 {
			t.Fatalf("change %d: %d computes without a change", i, n)
		}
	}
}

// AC 7: a compute is charged at least the minimum charge for each directory it stores.
func TestChargeAtLeastMinPerObject(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)

	// three distinct small directories: /a, /b and the root
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")

	full := bucketTokens(mod, vol.Identity)
	clk.Advance(5 * time.Second)
	if stepAll(mod) != 1 {
		t.Fatal("no compute")
	}

	if spent := full - bucketTokens(mod, vol.Identity); spent != 3*4096 {
		t.Fatalf("charged %v, want 3 x 4096", spent)
	}
}

// AC 8: the worker computes one VFS at a time.
func TestWorkerRunsOneVFSAtATime(t *testing.T) {
	mod := testModule(t)
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)

	vols := make([]*dbVolume, 20)
	for i := range vols {
		vols[i] = createVFS(t, mod, astral.GenerateIdentity())
		inVFS(t, mod, vols[i]).write("/f", "x")
	}
	repo.setOnCommit(func(astral.ObjectID) error {
		time.Sleep(time.Millisecond)
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.runDirWorker(mod.ctx)
	}()

	deadline := time.Now().Add(changeTestWait)
	for !allRootsCached(t, mod, vols) {
		if time.Now().After(deadline) {
			t.Fatal("the worker did not compute every VFS")
		}
		clk.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	mod.cancel()
	<-done

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.maxActive != 1 {
		t.Fatalf("%d stores at once, want 1", repo.maxActive)
	}
}

func allRootsCached(t *testing.T, mod *Module, vols []*dbVolume) bool {
	t.Helper()

	for _, vol := range vols {
		if cachedID(t, mod, vol.RootID) == nil {
			return false
		}
	}

	return true
}

// AC 8: a background compute starts no earlier than the end of the one before plus its charge at the node rate.
func TestNodeRateBound(t *testing.T) {
	mod := testModule(t)
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)

	vols := make([]*dbVolume, 10)
	for i := range vols {
		vols[i] = createVFS(t, mod, astral.GenerateIdentity())
		inVFS(t, mod, vols[i]).write("/a/f", "x")
		inVFS(t, mod, vols[i]).write("/b/f", string(rune('a'+i)))
	}
	clk.Advance(5 * time.Second)
	deadline := time.Now().Add(changeTestWait)
	for !allRootsCached(t, mod, vols) {
		if time.Now().After(deadline) {
			t.Fatal("the worker did not compute every VFS")
		}
		stepAll(mod)
		clk.Advance(10 * time.Millisecond)
	}

	// note: every compute stores at one time of the fake clock, and two computes never share one
	var times []time.Time
	charges := map[time.Time]uint64{}
	for _, c := range repo.directoryCommits() {
		if _, ok := charges[c.At]; !ok {
			times = append(times, c.At)
		}
		charges[c.At] += max(uint64(len(c.Bytes)), mod.config.DirectoryMinCharge)
	}
	if len(times) < len(vols) {
		t.Fatalf("%d computes, want %d", len(times), len(vols))
	}
	for i := 1; i < len(times); i++ {
		pace := time.Duration(float64(charges[times[i-1]]) / float64(mod.config.DirectoryNodeRate) * float64(time.Second))
		if times[i].Sub(times[i-1]) < pace {
			t.Fatalf("compute %d started %v after the one before, want at least %v", i, times[i].Sub(times[i-1]), pace)
		}
	}
}

// AC 8: of two due VFSes, the one due first is computed first, and the node pace holds the other back.
func TestMostOverdueComputedFirst(t *testing.T) {
	mod := testModule(t)
	clk := dirClock(t, mod)

	// why: the older VFS has the larger identity, so the order by identity alone would pick the other.
	older, newer := astral.GenerateIdentity(), astral.GenerateIdentity()
	if older.String() < newer.String() {
		older, newer = newer, older
	}
	first := createVFS(t, mod, older)
	inVFS(t, mod, first).write("/a/f", "x")
	clk.Advance(time.Second)
	second := createVFS(t, mod, newer)
	inVFS(t, mod, second).write("/a/f", "y")

	clk.Advance(5 * time.Second)
	if n := stepAll(mod); n != 1 {
		t.Fatalf("%d computes, want the 1 the node pace allows", n)
	}
	if cachedID(t, mod, first.RootID) == nil || cachedID(t, mod, second.RootID) != nil {
		t.Fatal("the VFS due later was computed first")
	}
}

// AC 9: a request spends no token and moves no node pace, also when the bucket holds fewer tokens than the
// compute costs.
func TestRequestSpendsNoToken(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a", "x")
	drainBucket(mod, vol.Identity)
	dirClock(t, mod).Advance(time.Second)

	tokens, pace := bucketTokens(mod, vol.Identity), mod.sched.nodeReadyAt()
	if tokens <= 0 || tokens >= float64(2*mod.config.DirectoryMinCharge) {
		t.Fatalf("bucket %v, want some tokens, fewer than /docs and the root cost", tokens)
	}

	entry := dirStat(t, mod, vol, "/docs")

	if !dirRepo(t, mod).has(entry.ID) {
		t.Fatal("the request did not store the directory")
	}
	if got := bucketTokens(mod, vol.Identity); got != tokens {
		t.Fatalf("bucket %v after the request, want %v", got, tokens)
	}
	if got := mod.sched.nodeReadyAt(); !got.Equal(pace) {
		t.Fatalf("node pace %v after the request, want %v", got, pace)
	}
}

// AC 9: a request on a VFS with an empty bucket is answered at once and spends no token.
func TestRequestOnEmptyBucketIsUnchargedAndImmediate(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a", "x")
	drainBucket(mod, vol.Identity)

	entry := dirStat(t, mod, vol, "/docs")

	if !dirRepo(t, mod).has(entry.ID) {
		t.Fatal("the answered directory is not stored")
	}
	if tokens := bucketTokens(mod, vol.Identity); tokens != 0 {
		t.Fatalf("bucket %v after the request, want 0", tokens)
	}
}

// AC 10: after a restart, a VFS with cleared ObjectIDs is computed without any request.
func TestRestartSeedsPendingVFSes(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/a/f", "x")

	again := &Module{config: defaultConfig, identity: mod.identity, log: log.New(nil), db: mod.db}
	setupDirTest(t, again)
	if err := again.seedPending(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if !isPending(again, vol.Identity) {
		t.Fatal("the VFS is not pending after the restart")
	}
	dirClock(t, again).Advance(5 * time.Second)
	if stepAll(again) != 1 {
		t.Fatal("not computed after the restart")
	}
	requireStoredRoot(t, again, vol)
}

// AC 11: a request for an ObjectID that is cached stores nothing.
func TestCachedIDStoresNothing(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a", "x")
	repo := dirRepo(t, mod)

	first := dirStat(t, mod, vol, "/docs")
	before := len(repo.directoryCommits())
	second := dirStat(t, mod, vol, "/docs")

	if second.ID != first.ID || len(repo.directoryCommits()) != before {
		t.Fatalf("the second stat stored %d directories", len(repo.directoryCommits())-before)
	}
}

// AC 12: a current directory survives the sweeper even when it is noted.
func TestCurrentListingSurvivesSweeper(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a", "x")
	entry := dirStat(t, mod, vol, "/docs")

	if err := mod.db.noteReplaced([]astral.ObjectID{entry.ID}, 0); err != nil {
		t.Fatal(err)
	}
	deleted, err := mod.sweepReplaced(mod.ctx, mod.clock.Now())
	if err != nil || deleted != 0 {
		t.Fatalf("sweep deleted %d, %v; want 0", deleted, err)
	}
	if !dirRepo(t, mod).has(entry.ID) {
		t.Fatal("the current directory was deleted")
	}
	if n := noteCount(t, mod); n != 0 {
		t.Fatalf("%d notes left, want the note of a held directory dropped", n)
	}
}

func noteCount(t *testing.T, mod *Module) int64 {
	t.Helper()

	var n int64
	if err := mod.db.Model(&dbReplacedDirectory{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n
}

func isNoted(t *testing.T, mod *Module, id astral.ObjectID) bool {
	t.Helper()

	var n int64
	if err := mod.db.Model(&dbReplacedDirectory{}).Where("object_id = ?", &id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n > 0
}

// AC 14: a failed store still answers the ObjectID, and a later compute stores the directory.
func TestStoreFailureAnswersIDAndRetries(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/docs/a", "x")
	repo := dirRepo(t, mod)
	clk := dirClock(t, mod)

	repo.setFailCommit(true)
	entry := dirStat(t, mod, vol, "/docs")
	docs := changeTestMustFind(t, mod, vol, "/docs")
	if docs.ObjectID != nil || len(repo.directoryCommits()) != 0 {
		t.Fatal("a failed store cached or stored a directory")
	}

	repo.setFailCommit(false)
	clk.Advance(mod.config.DirectoryLongestWait - time.Second)
	if stepAll(mod) != 0 {
		t.Fatal("retried before the longest wait")
	}
	clk.Advance(time.Second)
	if stepAll(mod) != 1 {
		t.Fatal("not retried after the longest wait")
	}

	if got := cachedID(t, mod, docs.ID); got == nil || *got != entry.ID || !repo.has(entry.ID) {
		t.Fatalf("cached %v, want the answered %v stored", got, entry.ID)
	}
	requireStoredRoot(t, mod, vol)
}

// AC 15: directories do not count in the usage of a VFS.
func TestListingsDoNotCountInUsage(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	inVFS(t, mod, vol).write("/a/b", "hello")
	inVFS(t, mod, vol).write("/c", "abc")

	for range 2 {
		info, err := mod.vfsInfo(vol)
		if err != nil {
			t.Fatal(err)
		}
		if info.Usage != 8 {
			t.Fatalf("usage %d, want 8", info.Usage)
		}
	}
}

// AC 16: a replaced directory that nothing holds is deleted after the grace time, within grace plus one interval.
func TestReplacedListingSweptWithinSeventyMinutes(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a", "1")
	old := dirStat(t, mod, vol, "/").ID
	inVFS(t, mod, vol).write("/b", "2")

	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.runDirSweeper(mod.ctx)
	}()
	defer func() { mod.cancel(); <-done }()

	for elapsed := time.Duration(0); elapsed < 70*time.Minute; elapsed += time.Minute {
		waitTimers(t, clk)
		clk.Advance(time.Minute)
		if elapsed < 59*time.Minute && !repo.has(old) {
			t.Fatalf("deleted after %v, before the grace time", elapsed+time.Minute)
		}
	}
	waitTimers(t, clk)

	if repo.has(old) || isNoted(t, mod, old) {
		t.Fatal("the replaced directory is kept after 70 minutes")
	}
}

// waitTimers waits until a goroutine waits on a timer of the fake clock.
func waitTimers(t *testing.T, clk *fakeClock) {
	t.Helper()

	deadline := time.Now().Add(changeTestWait)
	for clk.Timers() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no timer waits")
		}
		time.Sleep(time.Millisecond)
	}
}

// AC 16: the directories of a removed folder and of the folder below it are deleted after the grace time.
func TestRemovedFoldersSwept(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/b/f", "x")

	a := dirStat(t, mod, vol, "/a").ID
	b := cachedID(t, mod, changeTestMustFind(t, mod, vol, "/a/b").ID)
	if b == nil || !repo.has(a) || !repo.has(*b) {
		t.Fatal("/a and /a/b are not stored")
	}
	dirRemove(t, mod, vol, "/a")

	clk.Advance(mod.config.DirectoryGraceTime)
	deleted, err := mod.sweepReplaced(mod.ctx, clk.Now())
	if err != nil || deleted != 2 {
		t.Fatalf("sweep deleted %d, %v; want 2", deleted, err)
	}
	if repo.has(a) || repo.has(*b) {
		t.Fatal("a directory of a removed folder is kept")
	}
	if n := noteCount(t, mod); n != 0 {
		t.Fatalf("%d notes left, want 0", n)
	}
}

// AC 16: a replaced directory that is current again is kept.
func TestReplacedButCurrentAgainKept(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)

	empty := dirStat(t, mod, vol, "/").ID
	inVFS(t, mod, vol).write("/f", "x")
	withFile := dirStat(t, mod, vol, "/").ID
	dirRemove(t, mod, vol, "/f")
	if again := dirStat(t, mod, vol, "/").ID; again != empty {
		t.Fatalf("root %v after the remove, want the empty folder %v", again, empty)
	}

	clk.Advance(mod.config.DirectoryGraceTime)
	if _, err := mod.sweepReplaced(mod.ctx, clk.Now()); err != nil {
		t.Fatal(err)
	}

	if !repo.has(empty) {
		t.Fatal("the directory that is current again was deleted")
	}
	if repo.has(withFile) {
		t.Fatal("the replaced directory was kept")
	}
}

// AC 16: a replaced directory that another holder holds is kept.
func TestReplacedButOtherHolderKept(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	clk := dirClock(t, mod)
	inVFS(t, mod, vol).write("/f", "x")
	old := dirStat(t, mod, vol, "/").ID
	inVFS(t, mod, vol).write("/g", "y")

	mod.Objects.(*dirTestObjects).addHolder(holderFunc(func(id *astral.ObjectID) bool { return *id == old }))

	clk.Advance(mod.config.DirectoryGraceTime)
	if _, err := mod.sweepReplaced(mod.ctx, clk.Now()); err != nil {
		t.Fatal(err)
	}
	if !dirRepo(t, mod).has(old) {
		t.Fatal("a directory another holder holds was deleted")
	}
}

// AC 16: a noted directory that is already gone counts as deleted.
func TestSweepMissingObjectCountsAsDeleted(t *testing.T) {
	mod := testModule(t)
	gone := contentID("never stored")
	if err := mod.db.noteReplaced([]astral.ObjectID{gone}, 0); err != nil {
		t.Fatal(err)
	}

	deleted, err := mod.sweepReplaced(mod.ctx, mod.clock.Now())
	if err != nil || deleted != 1 || noteCount(t, mod) != 0 {
		t.Fatalf("deleted %d, %v, %d notes; want 1 and no note", deleted, err, noteCount(t, mod))
	}
}

// A note made again after the sweeper read its page keeps its grace time.
func TestSweeperSkipsNoteMadeAgainAfterItsPage(t *testing.T) {
	mod := testModule(t)
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)

	a, _ := repo.commit([]byte("listing-a"))
	b, _ := repo.commit([]byte("listing-b"))
	if err := mod.db.noteReplaced([]astral.ObjectID{*a, *b}, clk.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Hour)

	mod.Objects.(*dirTestObjects).addHolder(holderFunc(func(id *astral.ObjectID) bool {
		if *id == *a {
			// note: b is noted again now, while the sweeper works on a, after it read the page with both
			if err := mod.db.noteReplaced([]astral.ObjectID{*b}, clk.Now().UnixNano()); err != nil {
				t.Error(err)
			}
		}
		return false
	}))

	if _, err := mod.sweepReplaced(testContext(), clk.Now()); err != nil {
		t.Fatal(err)
	}
	if !repo.has(*b) {
		t.Fatal("the sweeper deleted b, noted again 0 s ago")
	}
	if !isNoted(t, mod, *b) {
		t.Fatal("the new note of b is gone")
	}
	if repo.has(*a) {
		t.Fatal("the sweeper kept a, whose grace time passed")
	}
}

// sweepWithWriter notes a listing, passes its grace time and sweeps it. Each holder check of the listing asks
// holds with the count of checks so far, from 1. It returns the listing.
func sweepWithWriter(t *testing.T, mod *Module, holds func(check int) bool) astral.ObjectID {
	t.Helper()

	clk := dirClock(t, mod)
	id, _ := dirRepo(t, mod).commit([]byte("listing"))
	if err := mod.db.noteReplaced([]astral.ObjectID{*id}, clk.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(mod.config.DirectoryGraceTime + time.Minute)

	checks := 0
	mod.Objects.(*dirTestObjects).addHolder(holderFunc(func(h *astral.ObjectID) bool {
		if *h != *id {
			return false
		}
		checks++
		return holds(checks)
	}))

	if _, err := mod.sweepReplaced(testContext(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	return *id
}

// A holder that appears after the sweeper's first holder check, before the delete, keeps the listing.
func TestSweeperKeepsListingHeldAfterCheck(t *testing.T) {
	mod := testModule(t)

	id := sweepWithWriter(t, mod, func(check int) bool { return check > 1 })

	if !dirRepo(t, mod).has(id) {
		t.Fatal("the sweeper deleted a listing held before its delete")
	}
	if !isNoted(t, mod, id) {
		t.Fatal("the listing kept for a late holder is not noted again")
	}
}

// A commit of the same bytes by a writer that takes no gate, after the sweeper's first holder check, keeps the
// listing: the writer records its holder only after the commit.
func TestSweeperKeepsListingCommittedAfterCheck(t *testing.T) {
	mod := testModule(t)
	repo := dirRepo(t, mod)

	id := sweepWithWriter(t, mod, func(check int) bool {
		if check == 1 {
			// note: stands for vfs.write or objects.store of the same bytes, before its entry exists
			repo.commit([]byte("listing"))
		}
		return false
	})

	if !repo.has(id) {
		t.Fatal("the sweeper deleted a listing committed again after the note")
	}
	if !isNoted(t, mod, id) {
		t.Fatal("the listing kept for a new commit is not noted again")
	}
}

// A folder that changes while its directory is stored keeps a cleared slot, and so does every folder above it.
// The untouched sibling is cached, and the stored directories no row took are noted.
func TestWritebackSkipsFolderChangedDuringStore(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")

	var once sync.Once
	repo.setOnCommit(func(astral.ObjectID) error {
		once.Do(func() { inVFS(t, mod, vol).write("/a/z", "3") })
		return nil
	})
	planned := dirStat(t, mod, vol, "/")
	repo.setOnCommit(nil)

	a, b := changeTestMustFind(t, mod, vol, "/a"), changeTestMustFind(t, mod, vol, "/b")
	if a.ObjectID != nil || cachedID(t, mod, vol.RootID) != nil {
		t.Fatal("a folder that changed during the store was cached")
	}
	if b.ObjectID == nil {
		t.Fatal("the untouched sibling was not cached")
	}
	if !isNoted(t, mod, planned.ID) {
		t.Fatal("the stored root no row took is not noted")
	}

	if _, err := mod.vfsInfo(vol); err != nil {
		t.Fatal(err)
	}
	requireStoredRoot(t, mod, vol)
}

// A folder whose directory below failed to store is neither stored nor cached.
func TestWritebackSkipsParentOfFailedChild(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")

	var failed astral.ObjectID
	var once sync.Once
	repo.setOnCommit(func(id astral.ObjectID) (err error) {
		once.Do(func() { failed, err = id, errors.New("refused") })
		return err
	})
	root := dirStat(t, mod, vol, "/")
	repo.setOnCommit(nil)

	if changeTestMustFind(t, mod, vol, "/a").ObjectID != nil || cachedID(t, mod, vol.RootID) != nil {
		t.Fatal("a folder with a failed store, or its parent, was cached")
	}
	if changeTestMustFind(t, mod, vol, "/b").ObjectID == nil {
		t.Fatal("the stored sibling was not cached")
	}
	if repo.has(failed) || repo.has(root.ID) {
		t.Fatal("the failed directory or its parent was stored")
	}
}

// A folder removed during a store and made anew under its name, on the reused row id, does not take the directory
// of the removed folder.
func TestWritebackSkipsReusedRowID(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/F/x", "1")
	old := changeTestMustFind(t, mod, vol, "/a/F")

	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo.setOnCommit(func(astral.ObjectID) error {
		once.Do(func() { close(reached); <-release })
		return nil
	})

	answered := make(chan astral.ObjectID, 1)
	go func() {
		id, err := mod.folderObjectID(vol.Identity, old.ID)
		if err != nil {
			t.Error(err)
		}
		answered <- id
	}()
	<-reached

	dirRemove(t, mod, vol, "/a/F")
	inVFS(t, mod, vol).write("/a/F/y", "2")
	made := changeTestMustFind(t, mod, vol, "/a/F")
	if made.ID != old.ID {
		t.Fatalf("the new folder is row %d, want the reused row %d", made.ID, old.ID)
	}

	repo.setOnCommit(nil)
	close(release)
	oldID := <-answered

	if got := cachedID(t, mod, made.ID); got != nil && *got == oldID {
		t.Fatalf("the new folder on row %d took the directory %v of the removed one", made.ID, oldID)
	}
}

// A store of an ID waits for its delete, and a delete is refused while a store holds the ID.
func TestGateExcludesStoreAndDelete(t *testing.T) {
	g := newDirGate()
	id := contentID("dir")

	g.beginStore(id)
	if g.tryBeginDelete(id) {
		t.Fatal("a delete began during a store")
	}
	g.endStore(id)

	if !g.tryBeginDelete(id) {
		t.Fatal("a delete was refused with no store")
	}
	stored := make(chan struct{})
	go func() {
		g.beginStore(id)
		close(stored)
	}()

	select {
	case <-stored:
		t.Fatal("a store began during a delete")
	case <-time.After(20 * time.Millisecond):
	}
	g.endDelete(id)
	<-stored
	g.endStore(id)
}

// A request that arrives during a compute of the same VFS waits for it and answers its result without storing.
func TestRequestSharesInFlightCompute(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")

	release := make(chan struct{})
	var once sync.Once
	entered := make(chan struct{})
	repo.setOnCommit(func(astral.ObjectID) error {
		once.Do(func() { close(entered); <-release })
		return nil
	})

	first := make(chan astral.ObjectID, 1)
	go func() {
		id, _ := mod.folderObjectID(vol.Identity, vol.RootID)
		first <- id
	}()
	<-entered

	second := make(chan astral.ObjectID, 1)
	go func() {
		id, _ := mod.folderObjectID(vol.Identity, vol.RootID)
		second <- id
	}()
	select {
	case <-second:
		t.Fatal("the second request did not wait for the compute")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	a, b := <-first, <-second
	if a != b {
		t.Fatalf("answers %v and %v, want one", a, b)
	}
	if n := len(repo.directoryCommits()); n != 3 {
		t.Fatalf("%d directories stored, want the 3 of one compute", n)
	}
}

// Two requests for different folders of one VFS compute one at a time, and both answer what is cached.
func TestTwoRequestsSameVFSSerialised(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")
	repo.setOnCommit(func(astral.ObjectID) error {
		time.Sleep(2 * time.Millisecond)
		return nil
	})

	rows := []int64{changeTestMustFind(t, mod, vol, "/a").ID, changeTestMustFind(t, mod, vol, "/b").ID}
	answers := make([]astral.ObjectID, 2)

	var wg sync.WaitGroup
	for i, row := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i], _ = mod.folderObjectID(vol.Identity, row)
		}()
	}
	wg.Wait()

	for i, row := range rows {
		if got := cachedID(t, mod, row); got == nil || *got != answers[i] {
			t.Fatalf("folder %d: cached %v, answered %v", i, got, answers[i])
		}
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.maxActive != 1 {
		t.Fatalf("%d stores at once, want 1", repo.maxActive)
	}
}

// A background compute in passes: a change during a later pass leaves the changed path to the next pass, and the
// compute ends with every folder cached as the rows give it.
func TestPassBoundaryChangeIsRecomputed(t *testing.T) {
	mod := testModule(t)
	mod.passBudget = 1
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	clk := dirClock(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")
	inVFS(t, mod, vol).write("/b/y", "2")

	commits := 0
	repo.setOnCommit(func(astral.ObjectID) error {
		if commits++; commits == 2 {
			// /a was cached by the first pass; this change clears it and the root again
			inVFS(t, mod, vol).write("/a/z", "3")
		}
		return nil
	})

	clk.Advance(5 * time.Second)
	if stepAll(mod) == 0 {
		t.Fatal("no compute")
	}
	if b := changeTestMustFind(t, mod, vol, "/b"); b.ObjectID == nil {
		t.Fatal("the untouched folder of the second pass is not cached")
	}
	if a := changeTestMustFind(t, mod, vol, "/a"); a.ObjectID != nil {
		t.Fatal("the folder changed during the pass is cached")
	}

	repo.setOnCommit(nil)
	clk.Advance(5 * time.Second)
	stepAll(mod)
	requireStoredRoot(t, mod, vol)
}

// Run starts the sweeper: a replaced directory past its grace time is deleted one sweep interval after Run.
func TestRunStartsSweeper(t *testing.T) {
	mod := testModule(t)
	clk := dirClock(t, mod)
	repo := dirRepo(t, mod)
	id, _ := repo.commit([]byte("listing"))
	if err := mod.db.noteReplaced([]astral.ObjectID{*id}, clk.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(mod.config.DirectoryGraceTime)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.Run(ctx)
	}()
	defer func() { cancel(); <-done }()

	waitTimers(t, clk)
	clk.Advance(mod.config.DirectorySweepInterval)

	deadline := time.Now().Add(changeTestWait)
	for repo.has(*id) {
		if time.Now().After(deadline) {
			t.Fatal("the sweeper Run started did not delete the replaced directory")
		}
		time.Sleep(time.Millisecond)
	}
}

// A new VFS is pending, so the background compute stores its root.
func TestCreateVFSMakesItPending(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	if !isPending(mod, vol.Identity) {
		t.Fatal("a new VFS is not pending")
	}
	dirClock(t, mod).Advance(mod.config.DirectoryQuietTime)
	if stepAll(mod) != 1 {
		t.Fatal("the new VFS was not computed")
	}
	requireStoredRoot(t, mod, vol)
}

// A request before Run stores its directory: the state of the stores exists from Load.
func TestRequestBeforeRunStores(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	info, err := mod.vfsInfo(vol)
	if err != nil {
		t.Fatal(err)
	}
	if !dirRepo(t, mod).has(info.Root) {
		t.Fatal("the root answered before Run is not stored")
	}
}

// vfs.move and vfs.touch answer a folder ObjectID that is stored.
func TestMoveTouchAnswerIsStored(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	inVFS(t, mod, vol).write("/a/x", "1")

	moved := changeTestEntry(t, changeTestCall(t, mod.OpMove, vol, map[string]any{"from": "/a", "to": "/c"}))
	if !repo.has(moved.ID) {
		t.Fatal("the moved folder's ObjectID is not stored")
	}

	inVFS(t, mod, vol).write("/c/y", "2")
	touched := changeTestEntry(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/c"}))
	if !repo.has(touched.ID) {
		t.Fatal("the touched folder's ObjectID is not stored")
	}
	if got := cachedID(t, mod, changeTestMustFind(t, mod, vol, "/c").ID); got == nil || *got != touched.ID {
		t.Fatalf("cached %v, want the answered %v", got, touched.ID)
	}
}

// A purge holds a leaf's lock while it asks the holders. Stores, write-backs and sweeps never wait on it while
// they hold the database, so the node never deadlocks.
func TestPurgeAskingHoldersNeverDeadlocks(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	repo := dirRepo(t, mod)
	clk := dirClock(t, mod)

	stop := make(chan struct{})
	purged := make(chan struct{})
	go func() {
		defer close(purged)
		id := contentID("x")
		for {
			select {
			case <-stop:
				return
			default:
			}
			repo.leaf.mu.Lock()
			mod.HoldObject(&id)
			repo.leaf.mu.Unlock()
		}
	}()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := range 20 {
			inVFS(t, mod, vol).write("/d/f", string(rune('a'+i)))
			mod.vfsInfo(vol)
			clk.Advance(mod.config.DirectoryGraceTime)
			mod.sweepReplaced(mod.ctx, clk.Now())
		}
	}()

	select {
	case <-finished:
	case <-time.After(changeTestWait):
		t.Fatal("deadlock: stores, write-backs and sweeps did not finish")
	}
	close(stop)
	<-purged
}
