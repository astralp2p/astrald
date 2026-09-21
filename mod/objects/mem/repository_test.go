package mem

import (
	"io"
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
