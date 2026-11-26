package apphost

import usermod "github.com/astralp2p/astrald/mod/user"

// why: core.Inject fills OptionalDeps.User only when the user module is assignable to
// hostUser, so a user module that stops satisfying it fails here and not at
// node start.
// why in a test: the package imports no more of mod/user than hostUser declares.
var _ hostUser = (usermod.Module)(nil)
