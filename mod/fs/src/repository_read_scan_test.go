package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

func TestRepository_ReadRange(t *testing.T) {
	repo := NewRepository(nil, "t", t.TempDir())
	id := commitString(t, repo, "hello")
	ctx := astral.NewContext(nil)

	for _, tc := range []struct {
		offset, limit int64
		want          string
	}{
		{offset: 1, limit: 3, want: "ell"},
		{offset: 0, limit: 0, want: "hello"},
	} {
		r, err := repo.Read(ctx, id, tc.offset, tc.limit)
		if err != nil {
			t.Errorf("Read(id, %d, %d): %v", tc.offset, tc.limit, err)
			continue
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Errorf("Read(id, %d, %d): ReadAll: %v", tc.offset, tc.limit, err)
			continue
		}
		if string(data) != tc.want {
			t.Errorf("Read(id, %d, %d): got %q, want %q", tc.offset, tc.limit, data, tc.want)
		}
	}
}

func TestRepository_ReadErrors(t *testing.T) {
	repo := NewRepository(nil, "t", t.TempDir())
	id := commitString(t, repo, "hello")
	ctx := astral.NewContext(nil)

	if _, err := repo.Read(ctx.WithZone(astral.ZoneNetwork), id, 0, 0); !errors.Is(err, astral.ErrZoneExcluded) {
		t.Errorf("Read in network zone: got %v, want %v", err, astral.ErrZoneExcluded)
	}

	missing := resolveString(t, "missing")
	if _, err := repo.Read(ctx, missing, 0, 0); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Read(missing): got %v, want %v", err, objects.ErrNotFound)
	}
}

func TestRepository_Contains(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "t", root)
	ctx := astral.NewContext(nil)
	id := commitString(t, repo, "hello")

	dirID := resolveString(t, "a directory")
	if err := os.Mkdir(filepath.Join(root, dirID.String()), 0o700); err != nil {
		t.Fatal(err)
	}

	if has, err := repo.Contains(ctx, id); err != nil || !has {
		t.Errorf("Contains(stored id): got %v, %v, want true, nil", has, err)
	}
	if has, err := repo.Contains(ctx, dirID); err != nil || has {
		t.Errorf("Contains(directory id): got %v, %v, want false, nil", has, err)
	}
}

func TestRepository_ScanSkipsNonObjects(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "t", root)
	id := commitString(t, repo, "hello")

	if err := os.WriteFile(filepath.Join(root, tempFilePrefix+"x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, resolveString(t, "a directory").String()), 0o700); err != nil {
		t.Fatal(err)
	}

	ch, err := repo.Scan(astral.NewContext(nil), false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var got []*astral.ObjectID
	for done := false; !done; {
		select {
		case scanned, ok := <-ch:
			if !ok {
				done = true
				break
			}
			got = append(got, scanned)
		case <-time.After(2 * time.Second):
			t.Fatal("Scan did not close within 2s")
		}
	}

	if len(got) != 1 || got[0] == nil || *got[0] != *id {
		t.Fatalf("Scan(follow=false): got %v, want [%v]", got, id)
	}
}
