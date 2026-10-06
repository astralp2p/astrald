package fs

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astralp2p/astrald/mod/objects/mem"
)

// TestOpen_MissingObjectIsNotExist opens a well-formed ID the repository does not
// hold. Open reports fs.ErrNotExist, and http.FileServer answers 404.
func TestOpen_MissingObjectIsNotExist(t *testing.T) {
	_, id := storeObject(t)
	fsys := NewFS(mem.New("empty", 0))

	_, err := fsys.Open(id.String())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open a missing object: err %v; want fs.ErrNotExist", err)
	}

	rec := httptest.NewRecorder()
	http.FileServer(http.FS(fsys)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+id.String(), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d; want %d", rec.Code, http.StatusNotFound)
	}
}
