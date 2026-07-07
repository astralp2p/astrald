package apphost

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/ipc"
	"github.com/cryptopunkscc/astrald/mod/objects/fs"
)

// HTTPAuthTokenHeader is the request header carrying the bearer token for HTTP clients.
const HTTPAuthTokenHeader = "X-Astral-Auth-Token"

// HTTPTargetHeader optionally overrides the query target identity for a request.
const HTTPTargetHeader = "X-Astral-Target"

// HTTPServer is the HTTP gateway for apphost, handling REST queries, object downloads,
// and WebSocket guest connections on a single listener.
type HTTPServer struct {
	*Module
	fileSystem *fs.FS
	fileServer http.Handler
	ctx        *astral.Context
}

func NewHTTPServer(mod *Module) *HTTPServer {
	srv := &HTTPServer{
		Module: mod,
	}

	srv.fileSystem = fs.NewFS(srv.Objects.ReadDefault())
	srv.fileServer = http.FileServer(http.FS(srv.fileSystem))

	return srv
}

// Run binds the HTTP listener and blocks until ctx is done.
// Returns nil immediately when BindHTTP is empty (HTTP disabled).
func (srv *HTTPServer) Run(ctx *astral.Context) error {
	srv.ctx = ctx

	// return if not configured
	bindHTTP := srv.config.BindHTTP
	if bindHTTP == "" {
		return nil
	}

	// bind to the address
	l, err := ipc.Listen(bindHTTP)
	if err != nil {
		srv.log.Error("http server: bind %v: %v", bindHTTP, err)
		return err
	}

	httpServer := &http.Server{Handler: srv}

	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := httpServer.Shutdown(sctx)
		if err != nil {
			srv.log.Error("http server: shutdown error at %v: %v", bindHTTP, err)
		} else {
			srv.log.Logv(1, "http server: stopped at %v", bindHTTP)
		}
	}()

	// start the server
	srv.log.Infov(1, "http server: started at %v", bindHTTP)
	err = httpServer.Serve(l)
	switch {
	case err == nil:
	case errors.Is(err, http.ErrServerClosed):
	default:
		srv.log.Error("http server: serve error at %v: %v", bindHTTP, err)
	}

	<-ctx.Done()
	return nil
}

func (srv *HTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// WebSocket upgrade has its own response shape (no CORS headers, in-protocol auth).
	if request.URL.Path == "/.ws" {
		srv.handleWS(writer, request)
		return
	}

	// add CORS headers
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", "*")

	// handle preflight OPTIONS request
	if request.Method == "OPTIONS" {
		writer.WriteHeader(http.StatusOK)
		return
	}

	var authToken = srv.getAuthToken(request)

	clientID, err := srv.AuthenticateToken(authToken)
	if err != nil {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	writer.Header().Set("X-Astral-Guest-Identity", clientID.String())
	writer.Header().Set("X-Astral-Host-Identity", srv.node.Identity().String())

	// handle object requests
	if after, found := strings.CutPrefix(request.URL.Path, "/.objects/"); found {
		// rewrite the path
		request.URL.Path = "/" + after
		NewHTTPObjectHandler(srv.Module, clientID).ServeHTTP(writer, request)
		return
	}

	NewHTTPQueryHandler(srv.Module, clientID).ServeHTTP(writer, request)
}

func (src *HTTPServer) getAuthToken(request *http.Request) (token string) {
	_, token, _ = strings.Cut(request.Header.Get("Authorization"), "Bearer ")
	return token
}
