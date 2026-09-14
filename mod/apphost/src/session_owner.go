package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// sessionOwner returns the authenticated identity of the guest session that
// sent q, or nil when that session presented no token.
//
// why: an op cannot see the session behind its query. routing.Op builds an
// IncomingQuery from the query and its origin alone, so the guest records the
// session identity on the en-route entry it keys by nonce, and the op reads it
// back here. A caller-supplied identity never establishes ownership.
//
// note: a query that did not arrive through a guest session - one off a link,
// or one this node issues itself - has no entry, and owns nothing.
//
// note: the entry lives only while the query is en route. An op calls this
// before it accepts, because accepting resolves the query and drops the entry.
func (mod *Module) sessionOwner(q *routing.IncomingQuery) *astral.Identity {
	er, found := mod.enRoute.Get(q.Nonce())
	if !found {
		return nil
	}

	return er.owner
}
