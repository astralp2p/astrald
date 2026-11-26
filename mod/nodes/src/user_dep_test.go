package nodes

import usermod "github.com/astralp2p/astrald/mod/user"

// why: core.Inject fills Deps.User only when the user module is assignable to
// userModule, so a user module that stops satisfying it fails here and not at
// node start.
// why in a test: the package imports no more of mod/user than userModule declares.
var _ userModule = (usermod.Module)(nil)
