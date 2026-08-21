package apphost

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

type HTTPQueryHandler struct {
	*Module
	Identity *astral.Identity
}

func NewHTTPQueryHandler(mod *Module, identity *astral.Identity) *HTTPQueryHandler {
	return &HTTPQueryHandler{Module: mod, Identity: identity}
}

func (srv *HTTPQueryHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var err error

	// establish the target identity
	targetID := srv.node.Identity()

	target := request.Header.Get(HTTPTargetHeader)
	if target != "" {
		targetID, err = astral.ParseIdentity(target)
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	// parse the query
	method, params := query.Parse(request.URL.String())
	method = strings.TrimPrefix(method, "/")

	if len(method) > 0 && method[0] == '@' {
		method = method[1:]
		split := strings.SplitN(method, "/", 2)
		if len(split) != 2 {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}

		targetID, err = srv.Dir.ResolveIdentity(split[0])
		if err != nil {
			writer.WriteHeader(http.StatusNotFound)
			return
		}

		method = split[1]
	}

	// check HTTP headers for application/json
	accept := request.Header.Get("Accept")
	if accept != "" && strings.Contains(accept, "application/json") {
		params["out"] = "json"
	}

	contentType := request.Header.Get("Content-Type")
	if contentType != "" && strings.Contains(contentType, "application/json") {
		params["in"] = "json"
	}

	// prepare the query context
	ctx, cancel := astral.
		NewContext(nil).
		WithZone(astral.ZoneAll).
		WithIdentity(srv.node.Identity()).
		WithTimeout(5 * time.Second)
	defer cancel()

	// route the query
	ch, err := query.Route(
		ctx,
		srv.node,
		query.New(srv.Identity, targetID, method, params),
	)
	switch {
	case err == nil:
	case errors.Is(err, &astral.ErrRejected{}):
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	case errors.Is(err, &astral.ErrRouteNotFound{}):
		writer.WriteHeader(http.StatusNotFound)
		return
	default:
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer ch.Close()

	// forward the traffic
	writer.WriteHeader(http.StatusOK)
	_, err = io.Copy(writer, ch.Transport())
}
