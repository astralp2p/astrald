package fs

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

func openStoredDigits(t *testing.T) (*File, *astral.ObjectID) {
	t.Helper()

	repo := mem.New("", 0)
	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	opened, err := NewFS(repo).Open(id.String())
	if err != nil {
		t.Fatalf("Open(%v): %v", id, err)
	}
	t.Cleanup(func() { opened.Close() })

	file, ok := opened.(*File)
	if !ok {
		t.Fatalf("Open returned %T, want *File", opened)
	}
	return file, id
}

func readRest(t *testing.T, r io.Reader) string {
	t.Helper()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func TestFS_OpenInvalidName(t *testing.T) {
	_, err := NewFS(mem.New("", 0)).Open("not-an-id")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open(\"not-an-id\"): got %v, want %v", err, fs.ErrNotExist)
	}
}

func TestFile_Stat(t *testing.T) {
	file, id := openStoredDigits(t)

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if got, want := info.Name(), id.String(); got != want {
		t.Errorf("Name(): got %q, want %q", got, want)
	}
	if got := info.Size(); got != 10 {
		t.Errorf("Size(): got %d, want 10", got)
	}
	if got := info.Mode(); got != 0444 {
		t.Errorf("Mode(): got %v, want %v", got, fs.FileMode(0444))
	}
	if info.IsDir() {
		t.Errorf("IsDir(): got true, want false")
	}
	if got, want := info.ModTime(), time.Unix(0, 0); !got.Equal(want) {
		t.Errorf("ModTime(): got %v, want %v", got, want)
	}
}

func TestFile_Seek(t *testing.T) {
	for _, tc := range []struct {
		name     string
		preread  int
		offset   int64
		whence   int
		wantPos  int64
		wantRest string
	}{
		{name: "start", offset: 4, whence: io.SeekStart, wantPos: 4, wantRest: "456789"},
		{name: "end", offset: -3, whence: io.SeekEnd, wantPos: 7, wantRest: "789"},
		{name: "current", preread: 3, offset: 2, whence: io.SeekCurrent, wantPos: 5, wantRest: "56789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, _ := openStoredDigits(t)

			if tc.preread > 0 {
				buf := make([]byte, tc.preread)
				if _, err := io.ReadFull(file, buf); err != nil {
					t.Fatalf("read %d bytes: %v", tc.preread, err)
				}
			}

			pos, err := file.Seek(tc.offset, tc.whence)
			if err != nil {
				t.Fatalf("Seek(%d, %d): %v", tc.offset, tc.whence, err)
			}
			if pos != tc.wantPos {
				t.Errorf("Seek(%d, %d): got position %d, want %d", tc.offset, tc.whence, pos, tc.wantPos)
			}
			if got := readRest(t, file); got != tc.wantRest {
				t.Errorf("read after Seek(%d, %d): got %q, want %q", tc.offset, tc.whence, got, tc.wantRest)
			}
		})
	}
}
