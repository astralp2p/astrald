package mem

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

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
