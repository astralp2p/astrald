package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astrald/mod/apphost"
)

func (mod *Module) GetAppRegisterPolicy() apphost.AppRegisterPolicy {
	return mod.AppRegisterAcceptAll
}

var _ apphost.AppRegisterPolicy = (*Module)(nil).AppRegisterAcceptAll

// AppRegisterAcceptAll admits every registration and grants only what the
// caller's origin is already entitled to. An app may ask for more; nothing
// here says yes. Granting a request needs a policy that decides to, which is
// where a node asks its user.
func (mod *Module) AppRegisterAcceptAll(origin string, requested []*auth.Permit) ([]*auth.Permit, bool) {
	granted := mod.GetWebOriginPermits(origin)

	mod.log.Info("accepting registration from origin %v: %v permits considered, %v granted",
		origin, len(requested), len(granted))

	return granted, true
}
