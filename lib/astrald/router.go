package astrald

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/apphost"
)

// Router is the core transport abstraction: it routes an in-flight query to its destination
// and exposes the local guest and host identities used to build queries.
type Router interface {
	RouteQuery(*astral.Context, *astral.InFlightQuery) (astral.Conn, error)
	GuestID() *astral.Identity
	HostID() *astral.Identity
}

var _ Router = &apphost.Router{}
