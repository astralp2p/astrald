package dir

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeConfigureNodeState reports whether the query's caller may change this
// node's alias table.
//
// dir.set_alias asks this question and rejects the query when the answer is no,
// before it accepts the connection or writes the alias. Clearing an alias is a
// change and asks the same question.
//
// why: the check sits at the op and not in Module.SetAlias, because SetAlias also
// serves in-process callers, which carry no query caller to authorize.
func (mod *Module) authorizeConfigureNodeState(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{
		Action: auth.NewAction(q.Caller()),
	})
}
