package apphost

import (
	"fmt"
	"os"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

// Router manages connections to an apphost endpoint, caching resolved identities
// across calls and handling context-driven query cancellation.
type Router struct {
	endpoint string
	token    string
	guestID  *astral.Identity
	hostID   *astral.Identity
}

var defaultRouter = newDefaultRouter()

func NewRouter(endpoint string, token string) *Router {
	return &Router{endpoint: endpoint, token: token}
}

func DefaultRouter() *Router {
	return defaultRouter
}

func SetDefaultRouter(router *Router) {
	defaultRouter = router
}

// RouteQuery routes a query via the host. Returns ErrNodeUnavailable if the
// IPC connection cannot be established (query was never sent, safe to retry).
func (router *Router) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery) (astral.Conn, error) {
	host, err := router.connect(ctx)
	if err != nil {
		return nil, err
	}

	// cancel the query when ctx ends
	var done = make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			cancelHost, err := router.connect(ctx)
			if err != nil {
				return
			}
			defer cancelHost.Close()

			conn, _ := cancelHost.RouteQuery(
				astral.Launch(query.New(nil, nil, apphost.MethodCancel, query.Args{"id": q.Nonce})),
				astral.ZoneDevice,
				nil,
			)
			if conn != nil {
				conn.Close()
			}

		case <-done:
		}
	}()

	return host.RouteQuery(q, ctx.Zone(), ctx.Filters())
}

// GuestID returns the authenticated guest identity, connecting to the host to
// resolve it on first call; returns nil if the connection or auth fails.
func (router *Router) GuestID() *astral.Identity {
	if router.guestID != nil {
		return router.guestID
	}

	host, err := router.connect(astral.NewContext(nil))
	if err != nil {
		return nil
	}
	defer host.Close()

	return router.guestID
}

// HostID returns the host node's identity, connecting to resolve it on first
// call; returns nil if the connection fails.
func (router *Router) HostID() *astral.Identity {
	if router.hostID != nil {
		return router.hostID
	}

	host, err := router.connect(astral.NewContext(nil))
	if err != nil {
		return nil
	}
	defer host.Close()

	return router.hostID
}

func (router *Router) Endpoint() string {
	return router.endpoint
}

func (router *Router) Protocol() string {
	split := strings.SplitN(router.endpoint, ":", 2)
	return split[0]
}

// connect makes a single attempt to connect and authenticate with the host.
// Returns ErrNodeUnavailable if the IPC dial fails.
func (router *Router) connect(ctx *astral.Context) (*Host, error) {
	host, err := Connect(ctx, router.endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNodeUnavailable, err)
	}

	router.hostID = host.HostID()

	if len(router.token) == 0 {
		return host, nil
	}

	err = host.AuthToken(router.token)
	if err != nil {
		host.Close()
		return nil, err
	}

	router.guestID = host.GuestID()
	return host, nil
}

func newDefaultRouter() *Router {
	// TODO: find an optimal endpoint (unix, then tcp)
	return NewRouter(DefaultEndpoint, os.Getenv(AuthTokenEnv))
}
