package apphost

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/objects/fs"
)

// HTTPObjectHandler serves stored objects over HTTP, guarding each request with
// an objects.ReadObjectAction authorization check before delegating to the embedded file server.
type HTTPObjectHandler struct {
	*Module
	Identity   *astral.Identity
	fileServer http.Handler
}

func NewHTTPObjectHandler(mod *Module, identity *astral.Identity) *HTTPObjectHandler {
	return &HTTPObjectHandler{
		Module:     mod,
		Identity:   identity,
		fileServer: http.FileServer(http.FS(fs.NewFS(mod.Objects.ReadDefault()))),
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

	if !srv.Deps.Auth.Authorize(ctx, &objects.ReadObjectAction{Action: auth.NewAction(srv.Identity), ObjectID: objectID}) {
		writer.WriteHeader(http.StatusForbidden)
		return
	}

	// pass the request to the file server
	writer.Header().Set("Content-Disposition", "inline; filename="+objectID.String())
	srv.fileServer.ServeHTTP(writer, request)
}
