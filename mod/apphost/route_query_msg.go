package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// RouteQueryMsg represents a request to route a query.
type RouteQueryMsg struct {
	Nonce   astral.Nonce
	Caller  *astral.Identity
	Target  *astral.Identity
	Query   astral.String16
	Zone    astral.Zone
	Filters []astral.String8
}

func (RouteQueryMsg) ObjectType() string { return "mod.apphost.route_query_msg" }

func (msg RouteQueryMsg) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&msg).WriteTo(w)
}

func (msg *RouteQueryMsg) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(msg).ReadFrom(r)
}

func init() {
	_ = astral.Add(&RouteQueryMsg{})
}
