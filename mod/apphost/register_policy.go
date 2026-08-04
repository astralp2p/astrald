package apphost

import "github.com/astralp2p/astral-go/api/auth"

// AppRegisterPolicy decides what apphost.register issues to the identity it is
// about to provision.
//
// origin is the caller's web origin, empty for local IPC callers. requested
// holds every permit under consideration: the ones a trusted web source
// entitles that origin to, followed by the ones the app asked for. granted is
// what registration writes into the node→app contract, and returning false
// refuses the registration outright.
//
// The two sources arrive in one list and a policy cannot tell them apart: a
// policy decides what an identity may hold, not where the asking came from.
// The shipped default grants everything it is handed, so a node that cares
// which apps hold what installs a policy that decides.
type AppRegisterPolicy func(origin string, requested []*auth.Permit) (granted []*auth.Permit, allow bool)
