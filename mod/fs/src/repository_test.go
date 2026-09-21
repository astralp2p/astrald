package fs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// Delete maps a missing-file os.Remove error to objects.ErrNotFound so purge skips the leaf.
func TestRepository_Delete_MissingFile(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	id := &astral.ObjectID{Size: 1}

	err := repo.Delete(nil, id)

	if !errors.Is(err, objects.ErrNotFound) {
		t.Fatalf("want objects.ErrNotFound, got %v", err)
	}
}

// Delete removes an existing object file and returns no error.
func TestRepository_Delete_ExistingFile(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	id := &astral.ObjectID{Size: 1}

	path := filepath.Join(root, id.String())
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(nil, id); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

// commit writes a payload through the repository writer without touching t,
// so it is safe to call from a goroutine.
func commit(repo *Repository, payload []byte) error {
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
	repo := NewRepository(nil, "test", t.TempDir())
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
			if err := commit(repo, []byte{byte(i), byte(i >> 8), 'p', 'a', 'y'}); err != nil {
				t.Errorf("writer %v: %v", i, err)
			}
		}(i)
	}

	writers.Wait()
	cancel()
	followers.Wait()
}

// TestReaderIDIsTheWholeObject: a reader over a window reports the stored object's full ID,
// and neither the Read argument nor a returned ID aliases the reader's copy.
func TestReaderIDIsTheWholeObject(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	ctx := astral.NewContext(nil)

	w, err := repo.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = w.Write([]byte("hello astral")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	arg := *id

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

// store commits a payload through the repository writer and returns its ID.
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

// resolveID returns the full ID of a payload.
func resolveID(t *testing.T, payload []byte) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	return id
}

// writeFile writes a payload to path, outside any repository writer.
func writeFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
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
func readAndClose(t *testing.T, r objects.Reader) []byte {
	t.Helper()
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	return data
}

// checkRead reads objectID whole and checks the bytes and the reader's full ID.
func checkRead(t *testing.T, repo objects.Repository, objectID *astral.ObjectID, want []byte) {
	t.Helper()

	r, err := repo.Read(astral.NewContext(nil), objectID, 0, 0)
	if err != nil {
		t.Fatalf("Read(%v): %v", objectID, err)
	}
	if got, full := r.ID(), resolveID(t, want); !got.IsEqual(full) {
		t.Errorf("Read(%v).ID() = %v, want %v", objectID, got, full)
	}
	if data := readAndClose(t, r); !bytes.Equal(data, want) {
		t.Errorf("Read(%v) = %q, want %q", objectID, data, want)
	}
}

// TestPartialLookupColdAndWarm: the first partial lookup builds the index, a commit adds to it,
// and a later hit is served from it without a refresh.
func TestPartialLookupColdAndWarm(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	ctx := astral.NewContext(nil)
	id := store(t, repo, []byte("hello astral"))

	if repo.ids != nil {
		t.Fatalf("index built before the first partial lookup")
	}

	checkRead(t, repo, partialOf(t, id), []byte("hello astral"))
	if repo.ids == nil {
		t.Fatalf("a cold partial lookup did not build the index")
	}
	var built = repo.ids

	other := store(t, repo, []byte("second"))
	if candidates, _ := repo.ids.Get(other.Hash); !slices.Equal(candidates, []astral.ObjectID{*other}) {
		t.Errorf("index after Commit holds %v for the new hash, want [%v]", candidates, other)
	}

	for _, want := range []*astral.ObjectID{id, other} {
		if ok, err := repo.Contains(ctx, partialOf(t, want)); !ok || err != nil {
			t.Errorf("Contains(partial %v) = %v, %v, want true, nil", want, ok, err)
		}
	}
	checkRead(t, repo, partialOf(t, other), []byte("second"))

	if repo.ids != built {
		t.Errorf("a warm hit refreshed the index")
	}
}

// TestPartialLookupFindsFileAddedAfterIndexing: a miss refreshes the index once, which finds a file
// added outside the repository writer.
func TestPartialLookupFindsFileAddedAfterIndexing(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)
	id := store(t, repo, []byte("hello astral"))

	if ok, err := repo.Contains(ctx, partialOf(t, id)); !ok || err != nil {
		t.Fatalf("Contains = %v, %v, want true, nil", ok, err)
	}
	var built = repo.ids

	payload := []byte("added outside")
	added := resolveID(t, payload)
	writeFile(t, filepath.Join(root, added.String()), payload)

	checkRead(t, repo, partialOf(t, added), payload)
	if repo.ids == built {
		t.Errorf("a miss did not refresh the index")
	}
}

// TestPartialLookupEvictsRemovedFile: a file removed outside the repository is a miss for every
// method, and its stale index entry is evicted.
func TestPartialLookupEvictsRemovedFile(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)
	id := store(t, repo, []byte("hello astral"))
	partial := partialOf(t, id)

	checkRead(t, repo, partial, []byte("hello astral"))

	if err := os.Remove(filepath.Join(root, id.String())); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Read(ctx, partial, 0, 0); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Read = %v, want ErrNotFound", err)
	}
	if _, found := repo.ids.Get(id.Hash); found {
		t.Errorf("the removed file is still indexed")
	}
	if ok, err := repo.Contains(ctx, partial); ok || err != nil {
		t.Errorf("Contains = %v, %v, want false, nil", ok, err)
	}
	if err := repo.Delete(ctx, partial); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Delete = %v, want ErrNotFound", err)
	}
}

// TestBadRecordedSize: a file whose length differs from the size in its name is not the object.
// Full-ID Contains keeps checking the file alone.
func TestBadRecordedSize(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)

	payload := []byte("hello astral")
	id := resolveID(t, payload)
	wrong := &astral.ObjectID{Size: id.Size + 1, Hash: id.Hash}
	writeFile(t, filepath.Join(root, wrong.String()), payload)

	for _, arg := range []*astral.ObjectID{partialOf(t, id), wrong} {
		if _, err := repo.Read(ctx, arg, 0, 0); !errors.Is(err, objects.ErrNotFound) {
			t.Errorf("Read(%v) = %v, want ErrNotFound", arg, err)
		}
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
		t.Errorf("Contains(partial) = %v, %v, want false, nil", ok, err)
	}
	if ok, err := repo.Contains(ctx, wrong); !ok || err != nil {
		t.Errorf("Contains(full) = %v, %v, want true, nil", ok, err)
	}
}

// TestNonCanonicalNamesAreNotObjects: a data0-named file and a data1 name with an extra leading 'y'
// parse to the object's hash, but Scan skips them and a partial lookup does not resolve them.
func TestNonCanonicalNamesAreNotObjects(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)

	payload := []byte("hello astral")
	id := resolveID(t, payload)
	for _, name := range []string{id.PartialString(), "data1y" + strings.TrimPrefix(id.String(), "data1")} {
		if parsed, err := astral.ParseID(name); err != nil || parsed.Hash != id.Hash {
			t.Fatalf("ParseID(%v) = %v, %v, want the hash of %v", name, parsed, err, id)
		}
		writeFile(t, filepath.Join(root, name), payload)
	}

	ch, err := repo.Scan(ctx, false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for emitted := range ch {
		t.Errorf("Scan emitted %v", emitted)
	}

	if _, err := repo.Read(ctx, partialOf(t, id), 0, 0); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Read = %v, want ErrNotFound", err)
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
		t.Errorf("Contains = %v, %v, want false, nil", ok, err)
	}
}

// TestOverLongNameIsNotAnObject: a data1 name longer than any canonical name is skipped by Scan and by the
// index listing. The longest canonical name is still an object file.
func TestOverLongNameIsNotAnObject(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)
	writeFile(t, filepath.Join(root, "data1"+strings.Repeat("b", 72)), []byte("x"))
	longest := &astral.ObjectID{Size: math.MaxUint64, Hash: [32]byte{1}}
	writeFile(t, filepath.Join(root, longest.String()), []byte("x"))

	absent := partialOf(t, resolveID(t, []byte("absent")))
	if ok, err := repo.Contains(ctx, absent); ok || err != nil {
		t.Errorf("Contains = %v, %v, want false, nil", ok, err)
	}

	ch, err := repo.Scan(ctx, false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var emitted []*astral.ObjectID
	for id := range ch {
		emitted = append(emitted, id)
	}
	if len(emitted) != 1 || !emitted[0].IsEqual(longest) {
		t.Errorf("Scan emitted %v, want only %v", emitted, longest)
	}
}

// TestFullIDReadMissTakesNoLock: a full ID names its file directly, so a Read that misses it does not wait
// for mu, which a partial lookup holds for a whole directory listing.
func TestFullIDReadMissTakesNoLock(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	payload := []byte("hello astral")
	id := resolveID(t, payload)
	wrong := &astral.ObjectID{Size: id.Size + 1, Hash: id.Hash}
	writeFile(t, filepath.Join(root, wrong.String()), payload)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	for _, arg := range []*astral.ObjectID{id, wrong} {
		done := make(chan error, 1)
		go func() {
			_, err := repo.Read(astral.NewContext(nil), arg, 0, 0)
			done <- err
		}()

		select {
		case err := <-done:
			if !errors.Is(err, objects.ErrNotFound) {
				t.Errorf("Read(%v) = %v, want ErrNotFound", arg, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Read(%v) waited for mu", arg)
		}
	}
}

// TestTwoSizesOfOneHashAreAmbiguous: two files that each match the size in their name and share a hash
// make a partial lookup ambiguous. Each full ID still reads its own file.
func TestTwoSizesOfOneHashAreAmbiguous(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)

	short := &astral.ObjectID{Size: 3, Hash: [32]byte{1, 2, 3}}
	long := &astral.ObjectID{Size: 5, Hash: short.Hash}
	writeFile(t, filepath.Join(root, short.String()), []byte("abc"))
	writeFile(t, filepath.Join(root, long.String()), []byte("abcde"))
	partial := partialOf(t, short)

	if _, err := repo.Read(ctx, partial, 0, 0); !errors.Is(err, objects.ErrAmbiguousObjectID) {
		t.Errorf("Read = %v, want ErrAmbiguousObjectID", err)
	}
	if ok, err := repo.Contains(ctx, partial); ok || !errors.Is(err, objects.ErrAmbiguousObjectID) {
		t.Errorf("Contains = %v, %v, want false, ErrAmbiguousObjectID", ok, err)
	}
	if err := repo.Delete(ctx, partial); !errors.Is(err, objects.ErrAmbiguousObjectID) {
		t.Errorf("Delete = %v, want ErrAmbiguousObjectID", err)
	}

	for id, want := range map[*astral.ObjectID]string{short: "abc", long: "abcde"} {
		r, err := repo.Read(ctx, id, 0, 0)
		if err != nil {
			t.Fatalf("Read(%v): %v", id, err)
		}
		if data := readAndClose(t, r); string(data) != want {
			t.Errorf("Read(%v) = %q, want %q", id, data, want)
		}
	}

	if err := os.Remove(filepath.Join(root, long.String())); err != nil {
		t.Fatal(err)
	}
	if r, err := repo.Read(ctx, partial, 0, 0); err != nil || !r.ID().IsEqual(short) {
		t.Errorf("Read after removing one candidate = %v, want the reader of %v", err, short)
	} else {
		r.Close()
	}
	if candidates, _ := repo.ids.Get(short.Hash); !slices.Equal(candidates, []astral.ObjectID{*short}) {
		t.Errorf("index after the hit holds %v, want the stale candidate evicted", candidates)
	}
}

// checkReadRanges reads windows of id, which names the payload "hello astral", by its full and its partial ID.
// A window outside the object is ErrOutOfBounds.
func checkReadRanges(t *testing.T, repo objects.Repository, id *astral.ObjectID) {
	t.Helper()

	for _, tc := range []struct {
		name    string
		offset  int64
		limit   int64
		want    string
		wantErr error
	}{
		{"whole object", 0, 0, "hello astral", nil},
		{"window", 2, 3, "llo", nil},
		{"limit past the end", 7, 100, "stral", nil},
		{"offset 1 and maximum limit", 1, math.MaxInt64, "ello astral", nil},
		{"offset at the end", 12, 0, "", nil},
		{"offset past the end", 13, 0, "", objects.ErrOutOfBounds},
		{"negative offset", -1, 0, "", objects.ErrOutOfBounds},
		{"negative limit", 0, -1, "", objects.ErrOutOfBounds},
	} {
		for name, arg := range map[string]*astral.ObjectID{"full": id, "partial": partialOf(t, id)} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				r, err := repo.Read(astral.NewContext(nil), arg, tc.offset, tc.limit)
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

// TestReadRanges: a full and a partial ID read the same windows.
func TestReadRanges(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())

	checkReadRanges(t, repo, store(t, repo, []byte("hello astral")))
}

// TestEmptyObject: the empty object's full ID has Size 0, so it resolves by hash.
func TestEmptyObject(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	ctx := astral.NewContext(nil)
	id := store(t, repo, nil)

	checkRead(t, repo, id, nil)
	if _, err := repo.Read(ctx, id, 1, 0); !errors.Is(err, objects.ErrOutOfBounds) {
		t.Errorf("Read at offset 1 = %v, want ErrOutOfBounds", err)
	}
	if err := repo.Delete(ctx, id); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if ok, err := repo.Contains(ctx, id); ok || err != nil {
		t.Errorf("Contains after Delete = %v, %v, want false, nil", ok, err)
	}
}

// TestDeleteByPartialID: Delete resolves a partial ID, removes the file and its index entry,
// and a later commit of the same content indexes it again.
func TestDeleteByPartialID(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)
	id := store(t, repo, []byte("hello astral"))
	partial := partialOf(t, id)

	if err := repo.Delete(ctx, partial); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, id.String())); !os.IsNotExist(err) {
		t.Errorf("the object file still exists: %v", err)
	}
	if _, found := repo.ids.Get(id.Hash); found {
		t.Errorf("the deleted object is still indexed")
	}
	if err := repo.Delete(ctx, partial); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if partial.Size != 0 || partial.Hash != id.Hash {
		t.Errorf("Delete changed its argument to %+v", partial)
	}

	store(t, repo, []byte("hello astral"))
	if candidates, _ := repo.ids.Get(id.Hash); !slices.Equal(candidates, []astral.ObjectID{*id}) {
		t.Errorf("index after a new Commit holds %v, want [%v]", candidates, id)
	}
}

// TestContainsFullAndPartialPaths: full-ID Contains answers false for every stat failure, while the
// partial path returns an operational error.
func TestContainsFullAndPartialPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "objects")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(nil, "test", root)
	ctx := astral.NewContext(nil)
	id := store(t, repo, []byte("hello astral"))

	dir := &astral.ObjectID{Size: 7, Hash: [32]byte{9}}
	if err := os.Mkdir(filepath.Join(root, dir.String()), 0o700); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Contains(ctx, dir); ok || err != nil {
		t.Errorf("Contains(directory) = %v, %v, want false, nil", ok, err)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Contains(ctx, id); ok || err != nil {
		t.Errorf("Contains(full) without a root = %v, %v, want false, nil", ok, err)
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err == nil || errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Contains(partial) without a root = %v, %v, want false and a filesystem error", ok, err)
	}
}

// TestCancelledRefreshKeepsIndex: a refresh cancelled by the context returns the context error
// and keeps the previous index.
func TestCancelledRefreshKeepsIndex(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	id := store(t, repo, []byte("hello astral"))
	checkRead(t, repo, partialOf(t, id), []byte("hello astral"))
	var built = repo.ids

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	absent := partialOf(t, resolveID(t, []byte("absent")))
	if ok, err := repo.Contains(ctx, absent); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("Contains with a cancelled context = %v, %v, want false, context.Canceled", ok, err)
	}
	if repo.ids != built {
		t.Errorf("a cancelled refresh replaced the index")
	}
	checkRead(t, repo, partialOf(t, id), []byte("hello astral"))
}

// TestConcurrentCommitDeleteAndPartialLookupAreRaceFree: writers, readers and deleters share each hash.
func TestConcurrentCommitDeleteAndPartialLookupAreRaceFree(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	ctx := astral.NewContext(nil)
	var workers sync.WaitGroup

	for i := 0; i < 8; i++ {
		payload := []byte{byte(i % 2), 'p', 'a', 'y'}
		id := resolveID(t, payload)
		partial := &astral.ObjectID{Hash: id.Hash}

		workers.Add(1)
		go func() {
			defer workers.Done()

			for turn := 0; turn < 20; turn++ {
				if err := commit(repo, payload); err != nil {
					t.Errorf("commit: %v", err)
					return
				}

				if r, err := repo.Read(ctx, partial, 0, 0); err == nil {
					if got := r.ID(); !got.IsEqual(id) {
						t.Errorf("ID() = %v, want %v", got, id)
					}
					r.Close()
				} else if !errors.Is(err, objects.ErrNotFound) {
					t.Errorf("Read: %v", err)
				}

				if _, err := repo.Contains(ctx, partial); err != nil {
					t.Errorf("Contains: %v", err)
				}

				if err := repo.Delete(ctx, partial); err != nil && !errors.Is(err, objects.ErrNotFound) {
					t.Errorf("Delete: %v", err)
				}
			}
		}()
	}

	workers.Wait()
}
