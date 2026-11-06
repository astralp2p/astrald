package apphost

import (
	"slices"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// gateAny is the gate entry that matches every op.
const gateAny = "*"

// gateAdmits reports whether the gate lets a session send q; tokenless names
// a session without a token. See Gate.
//
// why the node target for a session without a token: core.Router names an
// empty caller the node, so such a query runs with the node's authority, and
// an app must never receive a query nobody authenticated.
func (mod *Module) gateAdmits(ctx *astral.Context, q *astral.Query, tokenless bool) bool {
	op, _ := query.Parse(q.QueryString.String())
	toNode := q.Target.IsZero() || q.Target.IsEqual(mod.node.Identity())

	if tokenless && (!toNode || !gateLists(mod.config.Gate.Tokenless, op)) {
		return false
	}
	if !toNode {
		return true
	}
	return gateLists(mod.config.Gate.States.forState(mod.Sys.NodeState(ctx)), op)
}

// forState returns the op list of state; an unknown state lists nothing.
func (s GateStates) forState(state string) []string {
	switch state {
	case user.StateFresh:
		return s.Fresh
	case user.StateUnclaimed:
		return s.Unclaimed
	case user.StateClaimed:
		return s.Claimed
	}
	return nil
}

// gateLists reports whether list names op or holds gateAny.
func gateLists(list []string, op string) bool {
	return slices.Contains(list, gateAny) || slices.Contains(list, op)
}
