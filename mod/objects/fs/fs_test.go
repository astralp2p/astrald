package fs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

// storedData is the content of the object these tests store.
var storedData = []byte("partial object ids name an object by its hash alone")

// storeObject stores storedData in a memory repository and returns the repository and the object's full ID.
func storeObject(t *testing.T) (*mem.Repository, *astral.ObjectID) {
	t.Helper()

	repo := mem.New("test", 0)
	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.Write(storedData); err != nil {
		t.Fatalf("write: %v", err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	return repo, id
}

// partialNames returns the data0 name and the short data1 name of id's partial form.
func partialNames(id *astral.ObjectID) map[string]string {
	partial := astral.ObjectID{Hash: id.Hash}
	return map[string]string{
		"data0":       id.PartialString(),
		"short data1": partial.String(),
	}
}

// TestOpen_PartialNameReportsTheFullObject opens the object by a partial name. The
// file names the full ID, reports the full size, and seeks from the end.
func TestOpen_PartialNameReportsTheFullObject(t *testing.T) {
	repo, full := storeObject(t)

	for form, name := range partialNames(full) {
		t.Run(form, func(t *testing.T) {
			f, err := NewFS(repo).Open(name)
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer f.Close()

			info, err := f.Stat()
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if info.Size() != int64(len(storedData)) {
				t.Fatalf("stat size %d; want %d", info.Size(), len(storedData))
			}
			if info.Name() != full.String() {
				t.Fatalf("stat name %s; want the full ID %s", info.Name(), full)
			}

			seeker := f.(io.ReadSeeker)
			end, err := seeker.Seek(-4, io.SeekEnd)
			if err != nil {
				t.Fatalf("seek from the end: %v", err)
			}
			if end != int64(len(storedData))-4 {
				t.Fatalf("seek from the end landed at %d; want %d", end, len(storedData)-4)
			}

			tail, err := io.ReadAll(seeker)
			if err != nil {
				t.Fatalf("read the tail: %v", err)
			}
			if !bytes.Equal(tail, storedData[len(storedData)-4:]) {
				t.Fatalf("read the tail %q; want %q", tail, storedData[len(storedData)-4:])
			}
		})
	}
}

// TestFileServer_PartialNameServesTheFullLength serves the object by a partial name
// through http.FileServer, which reports the file's size as Content-Length.
func TestFileServer_PartialNameServesTheFullLength(t *testing.T) {
	repo, full := storeObject(t)
	server := http.FileServer(http.FS(NewFS(repo)))

	for form, name := range partialNames(full) {
		t.Run(form, func(t *testing.T) {
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d; want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(storedData)) {
				t.Fatalf("Content-Length %q; want %d", got, len(storedData))
			}
			if !bytes.Equal(rec.Body.Bytes(), storedData) {
				t.Fatalf("body %q; want %q", rec.Body.Bytes(), storedData)
			}
		})
	}
}

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
