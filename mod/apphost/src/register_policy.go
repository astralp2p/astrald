package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astrald/mod/apphost"
)

func (mod *Module) GetAppRegisterPolicy() apphost.AppRegisterPolicy {
	return mod.AppRegisterAcceptAll
}

var _ apphost.AppRegisterPolicy = (*Module)(nil).AppRegisterAcceptAll

// AppRegisterAcceptAll admits every registration and grants every permit put
// in front of it, whether the caller's origin entitled it or the app simply
// asked. It is the permissive default its name claims to be: a node that
// cares which apps hold what installs a policy that decides.
func (mod *Module) AppRegisterAcceptAll(origin string, requested []*auth.Permit) ([]*auth.Permit, bool) {
	mod.log.Info("accepting registration from origin %v with %v permits", origin, len(requested))

	return requested, true
}
