package apphost

import (
	iofs "io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects/fs"
)

// HTTPObjectHandler serves stored objects over HTTP, guarding each request with
// an auth.SeeObjectsAction authorization check before delegating to a file server.
type HTTPObjectHandler struct {
	*Module
	Identity *astral.Identity
	files    *fs.FS
}

func NewHTTPObjectHandler(mod *Module, identity *astral.Identity) *HTTPObjectHandler {
	return &HTTPObjectHandler{
		Module:   mod,
		Identity: identity,
		files:    fs.NewFS(mod.Objects.ReadDefault()),
	}
}

func (srv *HTTPObjectHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	filename := filepath.Base(request.URL.Path)

	objectID, err := astral.ParseID(filename)
	if err != nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	// authorize the handler to read the object
	ctx, cancel := astral.
		NewContext(nil).
		WithZone(astral.ZoneAll).
		WithIdentity(srv.node.Identity()).
		WithTimeout(5 * time.Second)
	defer cancel()

	if !srv.Deps.Auth.Authorize(ctx, &auth.SeeObjectsAction{Action: auth.NewAction(srv.Identity), ObjectID: objectID}) {
		writer.WriteHeader(http.StatusForbidden)
		return
	}

	// pass the request to the file server
	files := &dispositionFS{files: srv.files, header: writer.Header()}
	http.FileServer(http.FS(files)).ServeHTTP(writer, request)
}

// dispositionFS opens objects from files and names each opened object in header's Content-Disposition.
// why: a partial ID names no stored object until it opens, and the file server writes the headers after it opens the object.
type dispositionFS struct {
	files  *fs.FS
	header http.Header
}

func (d *dispositionFS) Open(name string) (iofs.File, error) {
	f, err := d.files.Open(name)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	d.header.Set("Content-Disposition", "inline; filename="+info.Name())

	return f, nil
}
