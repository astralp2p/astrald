package services

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

var _ core.QueryPreprocessor = &Module{}

// PreprocessQuery is Gate 1 for a query to a private service: a query whose
// target advertises a private service on this node passes only when its
// caller holds a discovery permit for one of them here, walked through the
// caller's contract chain. The service's app then decides what the caller may
// do there (Gate 2).
//
// why discovery: the permit the user approves for an app to reach a service
// names the service, and a query names only the provider's identity, so
// reaching a provider is allowed by a permit for a service it provides
// privately (vision: queries to services are authorized by the chain).
//
// note: the provider itself and this node pass, as does a target with no
// private service here: open services stay reachable as before.
func (mod *Module) PreprocessQuery(qm *core.QueryModifier) error {
	q := qm.Query()
	if q.Caller.IsZero() || q.Target.IsZero() || q.Caller.IsEqual(q.Target) || q.Caller.IsEqual(mod.node.Identity()) {
		return nil
	}

	names := mod.coord.PrivateNamesOf(q.Target)
	if len(names) == 0 {
		return nil
	}

	ctx := astral.NewContext(nil).WithIdentity(mod.node.Identity()).ExcludeZone(astral.ZoneNetwork)
	for _, name := range names {
		if mod.Auth.Authorize(ctx, &services.ServiceDiscoveryAction{
			Action:  auth.NewAction(q.Caller),
			Service: astral.String8(name),
			NodeID:  mod.node.Identity(),
		}) {
			return nil
		}
	}
	return astral.NewErrRejected(astral.DefaultRejectCode)
}
