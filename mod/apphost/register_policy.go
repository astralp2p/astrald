package apphost

import "github.com/astralp2p/astral-go/api/auth"

// AppRegisterPolicy decides whether apphost.register may provision a new app
// identity with the given permits. origin is the caller's web origin, empty
// for local IPC callers. permits is what registration issues to the new
// identity in a node→app contract (a trusted web source adds its permit
// template automatically); return true to allow the registration, false to
// refuse.
type AppRegisterPolicy func(origin string, permits []*auth.Permit) (allow bool)
