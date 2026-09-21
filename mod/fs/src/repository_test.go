package fs

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
