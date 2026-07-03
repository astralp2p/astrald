package apphost

import (
	"github.com/cryptopunkscc/astrald/mod/apphost"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

func (mod *Module) GetAppRegisterPolicy() apphost.AppRegisterPolicy {
	return mod.AppRegisterAcceptAll
}

var _ apphost.AppRegisterPolicy = (*Module)(nil).AppRegisterAcceptAll

func (mod *Module) AppRegisterAcceptAll(origin string, permits []*auth.Permit) bool {
	mod.log.Info("accepting registration from origin %v with %v permits", origin, len(permits))
	return true
}
