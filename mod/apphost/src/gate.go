package apphost

import (
	"slices"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// gateAny is the gate entry that matches every op.
const gateAny = "*"

// NodeState returns the node's state: claimed while a user contract is
// active; else unclaimed when a manager is appointed or the node was owned
// before; else fresh. It waits for the user module to finish initializing, so
// a query during startup on a claimed node is not wrongly restricted.
//
// why ownership history closes fresh: an expired or expelled claim must not
// reopen the appointment of a manager to the first local app.
// why ctx cancellation reads as fresh: fresh admits the fewest ops.
func (mod *Module) NodeState(ctx *astral.Context) string {
	if mod.User != nil {
		select {
		case <-mod.User.Ready():
		case <-ctx.Done():
			return user.StateFresh
		}
		if !mod.User.Identity().IsZero() {
			return user.StateClaimed
		}
		if mod.User.WasOwned() {
			return user.StateUnclaimed
		}
	}
	if !mod.Manager().IsZero() {
		return user.StateUnclaimed
	}
	return user.StateFresh
}

// Manager returns the App identity apphost.register_manager appointed, or nil.
func (mod *Module) Manager() *astral.Identity {
	return mod.appointment.Manager.Get()
}

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
	return gateLists(mod.config.Gate.States.forState(mod.NodeState(ctx)), op)
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
