package sys

import "github.com/astralp2p/astral-go/astral"

const ModuleName = "sys"

// Module is the public API surface of the sys module: the node's state and
// its manager. See .ai/system/protocols/sys/README.md.
type Module interface {
	// Manager returns the App identity sys.register_manager appointed, or nil.
	Manager() *astral.Identity

	// NodeState returns the node's state, one of user.StateFresh,
	// user.StateUnclaimed and user.StateClaimed.
	NodeState(ctx *astral.Context) string
}
