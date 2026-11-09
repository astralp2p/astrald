package apphost

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
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

	q := launchHTTPQuery(request, query.New(srv.Identity, targetID, method, params))

	// why: an HTTP session holds a token, and a query to the node obeys the
	// state lists of the gate as an IPC or WebSocket query does.
	if !srv.gateAdmits(ctx, q.Query, false) {
		writer.WriteHeader(http.StatusForbidden)
		return
	}

	// route the query
	conn, err := query.RouteInFlight(ctx, srv.node, q)
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
	defer conn.Close()

	// forward the traffic
	writer.WriteHeader(http.StatusOK)
	_, err = io.Copy(writer, conn)
}

// launchHTTPQuery launches q for an HTTP request and stamps the origin of the
// request on it.
func launchHTTPQuery(request *http.Request, q *astral.Query) *astral.InFlightQuery {
	inFlight := astral.Launch(q)

	// why: a bind_http on a public address admits clients from other machines.
	// Such a client arrives from outside the node, as a remote caller does, and
	// must not pass an op's refusal of network origin as a local caller would.
	if isRemote(request) {
		inFlight.Extra.Set("origin", astral.OriginNetwork)

		// why a mark besides the origin: network origin is also what a query
		// from a link peer carries, and an op that takes it as proof of a path
		// the peer's node vouched for would take this client's token as that
		// proof. The mark tells the two apart, and an op that must not accept
		// this client refuses it by the mark — messaging's send path does.
		inFlight.Extra.Set(apphostmod.ExtraHTTPRemote, true)
	}

	return inFlight
}

// isRemote reports whether request came over TCP from a non-loopback address.
//
// why TCP only: a unix: or memu: listener serves clients on the same host, and
// their peer address is not an IP. They stay local.
//
// why an unparsable TCP address is remote: isLoopback reads only a loopback IP
// as local, and a peer that cannot be placed is not trusted as local.
func isRemote(request *http.Request) bool {
	addr, ok := request.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	return ok && addr != nil && !isLoopback(request)
}
