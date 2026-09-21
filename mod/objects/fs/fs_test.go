package fs

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

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
