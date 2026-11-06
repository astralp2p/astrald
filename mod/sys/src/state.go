package sys

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// appointmentPath is where sys persists the appointed manager, at
// appointmentPath/manager.
const appointmentPath = "/mod/sys"

// legacyAppointmentPath is where apphost persisted the manager before sys
// owned it.
const legacyAppointmentPath = "/mod/apphost"

// appointmentConfig is bound to the tree at appointmentPath. An unset Manager
// is a node without a manager.
type appointmentConfig struct {
	Manager tree.Value[*astral.Identity]
}

// Manager returns the App identity sys.register_manager appointed, or nil.
//
// why the legacy record: a node apphost appointed must keep its manager, or
// it would read fresh and appoint a second one.
func (mod *Module) Manager() *astral.Identity {
	if m := mod.appointment.Manager.Get(); !m.IsZero() {
		return m
	}
	return mod.legacy.Manager.Get()
}

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
