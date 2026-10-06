package mem

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"testing"
	"time"

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

func storeBytes(t *testing.T, repo *Repository, data string) *astral.ObjectID {
	t.Helper()

	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("Write(%q): %v", data, err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return id
}

func resolveID(t *testing.T, data string) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatalf("Resolve(%q): %v", data, err)
	}
	return id
}

func receiveID(t *testing.T, ch <-chan *astral.ObjectID) (*astral.ObjectID, bool) {
	t.Helper()

	select {
	case id, ok := <-ch:
		return id, ok
	case <-time.After(2 * time.Second):
		t.Fatal("scan channel sent nothing within 2s")
		return nil, false
	}
}

func TestNew_Defaults(t *testing.T) {
	repo := New("", 0)

	if got, want := repo.Label(), "Memory"; got != want {
		t.Errorf("Label(): got %q, want %q", got, want)
	}

	free, err := repo.Free(astral.NewContext(nil))
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if free != DefaultSize {
		t.Errorf("Free(): got %d, want %d", free, DefaultSize)
	}
}

func TestRepository_CommitStoresContentAddressedObject(t *testing.T) {
	repo := New("", 0)
	ctx := astral.NewContext(nil)

	id := storeBytes(t, repo, "0123456789")

	if want := resolveID(t, "0123456789"); *id != *want {
		t.Fatalf("Commit id: got %v, want %v", id, want)
	}

	has, err := repo.Contains(ctx, id)
	if err != nil || !has {
		t.Fatalf("Contains(id): got %v, %v, want true, nil", has, err)
	}

	r, err := repo.Read(ctx, id, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got, want := string(data), "0123456789"; got != want {
		t.Fatalf("Read(id, 0, 0): got %q, want %q", got, want)
	}
}

func TestRepository_ReadBounds(t *testing.T) {
	repo := New("", 0)
	ctx := astral.NewContext(nil)
	id := storeBytes(t, repo, "0123456789")

	for _, tc := range []struct {
		offset, limit int64
		want          string
		wantErr       error
	}{
		{offset: 0, limit: 0, want: "0123456789"},
		{offset: 3, limit: 4, want: "3456"},
		{offset: 8, limit: 5, want: "89"},
		{offset: 10, limit: 0, want: ""},
		{offset: 11, limit: 0, wantErr: objectsmod.ErrOutOfBounds},
		{offset: -1, limit: 0, wantErr: objectsmod.ErrOutOfBounds},
		{offset: 0, limit: -1, wantErr: objectsmod.ErrOutOfBounds},
	} {
		r, err := repo.Read(ctx, id, tc.offset, tc.limit)
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Read(id, %d, %d): got error %v, want %v", tc.offset, tc.limit, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Read(id, %d, %d): unexpected error %v", tc.offset, tc.limit, err)
			continue
		}

		data, err := io.ReadAll(r)
		if err != nil {
			t.Errorf("Read(id, %d, %d): ReadAll: %v", tc.offset, tc.limit, err)
			continue
		}
		if string(data) != tc.want {
			t.Errorf("Read(id, %d, %d): got %q, want %q", tc.offset, tc.limit, data, tc.want)
		}
	}
}

func TestRepository_ReadAndDeleteErrors(t *testing.T) {
	repo := New("", 0)
	ctx := astral.NewContext(nil)
	id := storeBytes(t, repo, "0123456789")
	unknown := resolveID(t, "absent")

	if _, err := repo.Read(ctx.WithZone(astral.ZoneNetwork), id, 0, 0); !errors.Is(err, astral.ErrZoneExcluded) {
		t.Errorf("Read in network zone: got %v, want %v", err, astral.ErrZoneExcluded)
	}

	if _, err := repo.Read(ctx, unknown, 0, 0); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Read(unknown): got %v, want %v", err, objectsmod.ErrNotFound)
	}

	if err := repo.Delete(ctx, unknown); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Delete(unknown): got %v, want %v", err, objectsmod.ErrNotFound)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(id): got %v, want nil", err)
	}
	if has, _ := repo.Contains(ctx, id); has {
		t.Errorf("Contains(id) after Delete: got true, want false")
	}
}

func TestRepository_NoSpaceLeft(t *testing.T) {
	repo := New("m", 5)
	ctx := astral.NewContext(nil)

	if _, err := repo.Create(ctx, &objectsmod.CreateOpts{Alloc: 6}); !errors.Is(err, objectsmod.ErrNoSpaceLeft) {
		t.Errorf("Create(Alloc: 6) on size 5: got %v, want %v", err, objectsmod.ErrNoSpaceLeft)
	}

	w, err := repo.Create(ctx, &objectsmod.CreateOpts{Alloc: 5})
	if err != nil {
		t.Fatalf("Create(Alloc: 5) on size 5: %v", err)
	}
	if _, err := w.Write([]byte("012345")); !errors.Is(err, objectsmod.ErrNoSpaceLeft) {
		t.Errorf("Write of 6 bytes on size 5: got %v, want %v", err, objectsmod.ErrNoSpaceLeft)
	}
	if err := w.Discard(); err != nil {
		t.Errorf("Discard: %v", err)
	}
}

func TestWriter_ClosedStates(t *testing.T) {
	ctx := astral.NewContext(nil)

	t.Run("commit", func(t *testing.T) {
		repo := New("", 0)
		w, err := repo.Create(ctx, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := w.Write([]byte("abc")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if _, err := w.Commit(); err != nil {
			t.Fatalf("first Commit: %v", err)
		}

		if _, err := w.Commit(); !errors.Is(err, objectsmod.ErrClosedPipe) {
			t.Errorf("second Commit: got %v, want %v", err, objectsmod.ErrClosedPipe)
		}
		if _, err := w.Write([]byte("d")); !errors.Is(err, objectsmod.ErrClosedPipe) {
			t.Errorf("Write after Commit: got %v, want %v", err, objectsmod.ErrClosedPipe)
		}
	})

	t.Run("discard", func(t *testing.T) {
		repo := New("", 0)
		w, err := repo.Create(ctx, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := w.Write([]byte("abcd")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := repo.Used(); got != 4 {
			t.Fatalf("Used() before Discard: got %d, want 4", got)
		}

		if err := w.Discard(); err != nil {
			t.Fatalf("Discard: %v", err)
		}
		if got := repo.Used(); got != 0 {
			t.Errorf("Used() after Discard: got %d, want 0", got)
		}
		if err := w.Discard(); err != nil {
			t.Errorf("second Discard: got %v, want nil", err)
		}
	})
}

func TestReader_ClosedSeekAndRepo(t *testing.T) {
	repo := New("", 0)
	id := storeBytes(t, repo, "0123456789")

	r, err := repo.Read(astral.NewContext(nil), id, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got := r.Repo(); got != repo {
		t.Errorf("Repo(): got %v, want %v", got, repo)
	}

	reader, ok := r.(*Reader)
	if !ok {
		t.Fatalf("Read returned %T, want *Reader", r)
	}
	if _, err := reader.Seek(1, io.SeekStart); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Seek: got %v, want %v", err, errors.ErrUnsupported)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := r.Read(make([]byte, 4)); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Read after Close: got %v, want %v", err, os.ErrClosed)
	}
}

func TestRepository_ScanSnapshot(t *testing.T) {
	repo := New("", 0)
	a := storeBytes(t, repo, "a")
	b := storeBytes(t, repo, "b")

	ch, err := repo.Scan(astral.NewContext(nil), false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := map[astral.ObjectID]int{}
	for {
		id, ok := receiveID(t, ch)
		if !ok {
			break
		}
		if id == nil {
			t.Fatal("Scan(follow=false) sent a nil id")
		}
		got[*id]++
	}

	want := map[astral.ObjectID]int{*a: 1, *b: 1}
	if len(got) != len(want) || got[*a] != 1 || got[*b] != 1 {
		t.Fatalf("Scan(follow=false): got %v, want %v", got, want)
	}
}

func TestRepository_ScanFollow(t *testing.T) {
	repo := New("", 0)
	a := storeBytes(t, repo, "a")
	b := storeBytes(t, repo, "b")

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	ch, err := repo.Scan(ctx, true)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := map[astral.ObjectID]int{}
	for {
		id, ok := receiveID(t, ch)
		if !ok {
			t.Fatal("Scan(follow=true) closed before the nil boundary")
		}
		if id == nil {
			break
		}
		got[*id]++
	}
	if len(got) != 2 || got[*a] != 1 || got[*b] != 1 {
		t.Fatalf("Scan(follow=true) snapshot: got %v, want %v and %v once each", got, a, b)
	}

	c := storeBytes(t, repo, "c")
	id, ok := receiveID(t, ch)
	if !ok || id == nil || *id != *c {
		t.Fatalf("Scan(follow=true) after the boundary: got %v (open %v), want %v", id, ok, c)
	}

	cancel()
	for {
		_, ok := receiveID(t, ch)
		if !ok {
			break
		}
	}
}
