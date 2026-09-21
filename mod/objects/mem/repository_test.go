package mem

import (
	"bytes"
	"errors"
	"io"
	"math"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

func store(t *testing.T, repo *Repository, payload []byte) *astral.ObjectID {
	t.Helper()

	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err = w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	return id
}

func TestDeleteFreesQuota(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)

	var payload = []byte("hello astral")
	var id = store(t, repo, payload)

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	free, err := repo.Free(ctx)
	if err != nil {
		t.Fatalf("Free: %v", err)
	}

	if free != 1024 {
		t.Errorf("Free after deleting the only object = %v, want 1024", free)
	}
	if used := repo.Used(); used != 0 {
		t.Errorf("Used after deleting the only object = %v, want 0", used)
	}
}

func TestDeleteMissingObjectKeepsQuota(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)

	var id = store(t, repo, []byte("hello astral"))

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// a second Delete of the same object finds nothing and releases nothing
	if err := repo.Delete(ctx, id); err != objectsmod.ErrNotFound {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}

	if used := repo.Used(); used != 0 {
		t.Errorf("Used after one delete and one miss = %v, want 0", used)
	}
}

func TestDuplicateCommitHoldsQuotaOnce(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)

	var payload = []byte("hello astral")
	var id = store(t, repo, payload)
	var used = repo.Used()

	// the same content resolves to the same object and stores no second copy
	if again := store(t, repo, payload); again.String() != id.String() {
		t.Fatalf("second Commit = %v, want %v", again, id)
	}

	if got := repo.Used(); got != used {
		t.Errorf("Used after storing identical content twice = %v, want %v", got, used)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := repo.Used(); got != 0 {
		t.Errorf("Used after deleting the object = %v, want 0", got)
	}
}

func TestDiscardFreesQuota(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)

	w, err := repo.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = w.Write([]byte("hello astral")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err = w.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if used := repo.Used(); used != 0 {
		t.Errorf("Used after Discard = %v, want 0", used)
	}
}

func TestStoreDeleteLoopKeepsQuota(t *testing.T) {
	var repo = New("test", 256)
	var ctx = astral.NewContext(nil)

	// the repository holds nothing at the end of each turn, so the quota never runs out
	for i := 0; i < 200; i++ {
		var payload = []byte{byte(i), byte(i >> 8), 'x', 'y', 'z'}

		w, err := repo.Create(ctx, nil)
		if err != nil {
			t.Fatalf("Create at turn %v: %v", i, err)
		}
		if _, err = w.Write(payload); err != nil {
			t.Fatalf("Write at turn %v: %v", i, err)
		}
		id, err := w.Commit()
		if err != nil {
			t.Fatalf("Commit at turn %v: %v", i, err)
		}
		if err = repo.Delete(ctx, id); err != nil {
			t.Fatalf("Delete at turn %v: %v", i, err)
		}
	}

	if used := repo.Used(); used != 0 {
		t.Errorf("Used after 200 store/delete turns = %v, want 0", used)
	}
}

func TestWriteHoldsSizeUnderConcurrency(t *testing.T) {
	var repo = New("test", 100)
	var ctx = astral.NewContext(nil)

	var start = make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			w, err := repo.Create(ctx, nil)
			if err != nil {
				return
			}

			<-start
			if _, err = w.Write([]byte{byte(i), byte(i >> 8), 'p', 'a', 'y', 'l', 'o', 'a', 'd', '!'}); err != nil {
				w.Discard()
			}
		}(i)
	}

	close(start)
	wg.Wait()

	if used := repo.Used(); used > 100 {
		t.Errorf("Used under 50 concurrent writers = %v, want no more than the size, 100", used)
	}
}

// storeErr commits a payload without touching t, so it is safe to call from a goroutine.
func storeErr(repo *Repository, payload []byte) error {
	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		return err
	}
	if _, err = w.Write(payload); err != nil {
		return err
	}
	_, err = w.Commit()
	return err
}

func TestConcurrentCommitAndScanAreRaceFree(t *testing.T) {
	var repo = New("test", 1<<20)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	var followers, writers sync.WaitGroup

	for i := 0; i < 4; i++ {
		followers.Add(1)
		go func() {
			defer followers.Done()

			ch, err := repo.Scan(ctx, true)
			if err != nil {
				t.Errorf("Scan: %v", err)
				return
			}
			for range ch {
			}
		}()
	}

	for i := 0; i < 16; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()

			// distinct content per writer, so every Commit stores and notifies
			if err := storeErr(repo, []byte{byte(i), byte(i >> 8), 'p', 'a', 'y'}); err != nil {
				t.Errorf("writer %v: %v", i, err)
			}
		}(i)
	}

	writers.Wait()
	cancel()
	followers.Wait()

	if got := repo.objects.Len(); got != 16 {
		t.Errorf("stored objects = %v, want 16", got)
	}
}

// TestReaderIDIsTheWholeObject: a reader over a window reports the stored object's full ID,
// and neither the Read argument nor a returned ID aliases the reader's copy.
func TestReaderIDIsTheWholeObject(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)

	var id = store(t, repo, []byte("hello astral"))
	var arg = *id

	r, err := repo.Read(ctx, &arg, 2, 3)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "llo" {
		t.Fatalf("window = %q, want %q", data, "llo")
	}

	arg.Size++
	r.ID().Size++

	if got := r.ID(); !got.IsEqual(id) {
		t.Fatalf("ID() = %v, want %v", got, id)
	}
}

// partialOf returns the partial ID of id, parsed from its data0 form.
func partialOf(t *testing.T, id *astral.ObjectID) *astral.ObjectID {
	t.Helper()

	partial, err := astral.ParseID(id.PartialString())
	if err != nil {
		t.Fatalf("ParseID(%v): %v", id.PartialString(), err)
	}
	if partial.Size != 0 || partial.Hash != id.Hash {
		t.Fatalf("ParseID(%v) = %+v, want the partial ID of %v", id.PartialString(), partial, id)
	}

	return partial
}

// readAndClose reads r to the end and closes it.
func readAndClose(t *testing.T, r objectsmod.Reader) []byte {
	t.Helper()
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	return data
}

// TestReadRanges: a full and a partial ID read the same windows, and each reader reports the full ID.
func TestReadRanges(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)
	var id = store(t, repo, []byte("hello astral"))

	for _, tc := range []struct {
		name    string
		offset  int64
		limit   int64
		want    string
		wantErr error
	}{
		{"whole object", 0, 0, "hello astral", nil},
		{"window", 2, 3, "llo", nil},
		{"rest from offset", 6, 0, "astral", nil},
		{"limit past the end", 7, 100, "stral", nil},
		{"offset 1 and maximum limit", 1, math.MaxInt64, "ello astral", nil},
		{"offset at the end", 12, 0, "", nil},
		{"offset at the end with a limit", 12, 5, "", nil},
		{"offset past the end", 13, 0, "", objectsmod.ErrOutOfBounds},
		{"negative offset", -1, 0, "", objectsmod.ErrOutOfBounds},
		{"negative limit", 0, -1, "", objectsmod.ErrOutOfBounds},
	} {
		for name, arg := range map[string]*astral.ObjectID{"full": id, "partial": partialOf(t, id)} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				var before = *arg

				r, err := repo.Read(ctx, arg, tc.offset, tc.limit)
				if *arg != before {
					t.Errorf("Read changed its argument to %+v, want %+v", *arg, before)
				}
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("Read(%v, %v) error = %v, want %v", tc.offset, tc.limit, err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("Read(%v, %v): %v", tc.offset, tc.limit, err)
				}
				if got := r.ID(); !got.IsEqual(id) {
					t.Errorf("ID() = %v, want %v", got, id)
				}
				if data := readAndClose(t, r); string(data) != tc.want {
					t.Errorf("Read(%v, %v) = %q, want %q", tc.offset, tc.limit, data, tc.want)
				}
			})
		}
	}
}

// TestEmptyObject: the empty object's full ID has Size 0, so its full and partial IDs are one value.
func TestEmptyObject(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)
	var id = store(t, repo, nil)

	if id.Size != 0 {
		t.Fatalf("empty object ID = %v, want Size 0", id)
	}

	r, err := repo.Read(ctx, partialOf(t, id), 0, 512)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := r.ID(); !got.IsEqual(id) {
		t.Errorf("ID() = %v, want %v", got, id)
	}
	if data := readAndClose(t, r); len(data) != 0 {
		t.Errorf("Read = %q, want no bytes", data)
	}

	if _, err = repo.Read(ctx, id, 1, 0); !errors.Is(err, objectsmod.ErrOutOfBounds) {
		t.Errorf("Read at offset 1 = %v, want ErrOutOfBounds", err)
	}

	if ok, err := repo.Contains(ctx, id); !ok || err != nil {
		t.Errorf("Contains = %v, %v, want true, nil", ok, err)
	}

	if err = repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, err := repo.Contains(ctx, id); ok || err != nil {
		t.Errorf("Contains after Delete = %v, %v, want false, nil", ok, err)
	}
}

// TestFullIDOfAnotherSizeMisses: a full ID matches only a stored object of its size.
func TestFullIDOfAnotherSizeMisses(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)
	var id = store(t, repo, []byte("hello astral"))
	var used = repo.Used()

	for _, size := range []uint64{id.Size - 1, id.Size + 1} {
		var other = &astral.ObjectID{Size: size, Hash: id.Hash}

		if _, err := repo.Read(ctx, other, 0, 0); !errors.Is(err, objectsmod.ErrNotFound) {
			t.Errorf("Read(%v) = %v, want ErrNotFound", other, err)
		}
		if ok, err := repo.Contains(ctx, other); ok || err != nil {
			t.Errorf("Contains(%v) = %v, %v, want false, nil", other, ok, err)
		}
		if err := repo.Delete(ctx, other); !errors.Is(err, objectsmod.ErrNotFound) {
			t.Errorf("Delete(%v) = %v, want ErrNotFound", other, err)
		}
	}

	if ok, err := repo.Contains(ctx, id); !ok || err != nil {
		t.Errorf("Contains(%v) after the misses = %v, %v, want true, nil", id, ok, err)
	}
	if got := repo.Used(); got != used {
		t.Errorf("Used after the misses = %v, want %v", got, used)
	}
}

// TestPartialIDLookup: a partial ID finds the stored object by hash, and a partial ID of an
// absent hash misses in each method's own way. No method changes its argument.
func TestPartialIDLookup(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)
	var id = store(t, repo, []byte("hello astral"))
	var other = store(t, New("other", 1024), []byte("absent"))

	var hit = partialOf(t, id)
	var miss = partialOf(t, other)

	if ok, err := repo.Contains(ctx, hit); !ok || err != nil {
		t.Errorf("Contains(hit) = %v, %v, want true, nil", ok, err)
	}
	if ok, err := repo.Contains(ctx, miss); ok || err != nil {
		t.Errorf("Contains(miss) = %v, %v, want false, nil", ok, err)
	}
	if _, err := repo.Read(ctx, miss, 0, 0); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Read(miss) = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, miss); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Delete(miss) = %v, want ErrNotFound", err)
	}

	if hit.Size != 0 || hit.Hash != id.Hash || miss.Size != 0 || miss.Hash != other.Hash {
		t.Errorf("lookups changed their arguments to %+v and %+v", hit, miss)
	}
}

// TestPartialReaderIDIsTheWholeObject: a reader opened by a partial ID over a window reports the
// stored object's full ID, and the partial argument stays partial.
func TestPartialReaderIDIsTheWholeObject(t *testing.T) {
	var repo = New("test", 1024)
	var id = store(t, repo, []byte("hello astral"))
	var arg = partialOf(t, id)

	r, err := repo.Read(astral.NewContext(nil), arg, 2, 3)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := r.ID(); !got.IsEqual(id) {
		t.Errorf("ID() = %v, want %v", got, id)
	}
	if data := readAndClose(t, r); string(data) != "llo" {
		t.Errorf("window = %q, want %q", data, "llo")
	}
	if arg.Size != 0 {
		t.Errorf("Read set the argument's Size to %v", arg.Size)
	}
}

// TestDeleteByPartialID: a partial ID deletes the stored object and releases its bytes once.
func TestDeleteByPartialID(t *testing.T) {
	var repo = New("test", 1024)
	var ctx = astral.NewContext(nil)
	var id = store(t, repo, []byte("hello astral"))
	var arg = partialOf(t, id)

	if err := repo.Delete(ctx, arg); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, arg); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if ok, err := repo.Contains(ctx, id); ok || err != nil {
		t.Errorf("Contains(full) after Delete = %v, %v, want false, nil", ok, err)
	}
	if used := repo.Used(); used != 0 {
		t.Errorf("Used after Delete = %v, want 0", used)
	}
	if arg.Size != 0 || arg.Hash != id.Hash {
		t.Errorf("Delete changed its argument to %+v", arg)
	}
}

// TestScanEmitsFullIDs: Scan reports each stored object by its full ID.
func TestScanEmitsFullIDs(t *testing.T) {
	var repo = New("test", 1024)
	var want = map[astral.ObjectID]bool{
		*store(t, repo, []byte("hello astral")): true,
		*store(t, repo, []byte("x")):            true,
		*store(t, repo, nil):                    true,
	}

	ch, err := repo.Scan(astral.NewContext(nil), false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var got = map[astral.ObjectID]bool{}
	for id := range ch {
		got[*id] = true
	}

	if len(got) != len(want) {
		t.Fatalf("Scan emitted %v IDs, want %v", len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Errorf("Scan did not emit %v", &id)
		}
	}
}

// churnObject commits a payload, reads and checks it by its partial ID, and deletes it, 50 times.
// churnObject reports through t.Errorf, so it is safe to call from a goroutine.
func churnObject(t *testing.T, repo *Repository, payload []byte) {
	var ctx = astral.NewContext(nil)
	var id, _ = astral.Resolve(bytes.NewReader(payload))
	var partial = &astral.ObjectID{Hash: id.Hash}

	for turn := 0; turn < 50; turn++ {
		if err := storeErr(repo, payload); err != nil {
			t.Errorf("store: %v", err)
			return
		}

		if r, err := repo.Read(ctx, partial, 0, 0); err == nil {
			if got := r.ID(); !got.IsEqual(id) {
				t.Errorf("ID() = %v, want %v", got, id)
			}
			r.Close()
		} else if !errors.Is(err, objectsmod.ErrNotFound) {
			t.Errorf("Read: %v", err)
		}

		if _, err := repo.Contains(ctx, partial); err != nil {
			t.Errorf("Contains: %v", err)
		}

		if err := repo.Delete(ctx, partial); err != nil && !errors.Is(err, objectsmod.ErrNotFound) {
			t.Errorf("Delete: %v", err)
		}
	}
}

// TestConcurrentCommitDeleteAndPartialLookupAreRaceFree: four workers share each hash. A reader reports
// the full ID, and the quota drains to zero once every object is deleted.
func TestConcurrentCommitDeleteAndPartialLookupAreRaceFree(t *testing.T) {
	var repo = New("test", 1<<20)
	var workers sync.WaitGroup

	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()

			churnObject(t, repo, []byte{byte(i % 4), 'p', 'a', 'y'})
		}()
	}

	workers.Wait()

	for hash := range repo.objects.Clone() {
		if err := repo.Delete(astral.NewContext(nil), &astral.ObjectID{Hash: hash}); err != nil {
			t.Errorf("final Delete: %v", err)
		}
	}
	if used := repo.Used(); used != 0 {
		t.Errorf("Used after deleting every object = %v, want 0", used)
	}
}
