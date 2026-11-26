package services

import (
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

var _ core.QueryPreprocessor = &Module{}

// PreprocessQuery is Gate 1 for a query to a private service: a query whose
// target advertises a private service on this node passes only when its
// caller holds a discovery permit here, walked through the caller's contract
// chain, for the service the query reaches. The service's app then decides
// what the caller may do there (Gate 2).
//
// The service a query reaches is the target's private service its path names:
// the path is the name, or the name followed by "." and an op, the longest
// such name when several match. A query whose path names none of them needs a
// permit for any one of them, as before.
//
// why discovery: the permit the user approves for an app to reach a service
// names the service (vision: queries to services are authorized by the
// chain).
//
// why the named service: a permit for one private service of a provider
// must not reach another one it provides.
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

	if name, ok := reachedService(string(q.QueryString), names); ok {
		names = []string{name}
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

// reachedService returns the name among names that the query string's path
// names: the path itself, or a prefix of it followed by ".", the longest one
// when several do.
func reachedService(queryString string, names []string) (string, bool) {
	path, _, _ := strings.Cut(queryString, "?")
	var best string
	for _, name := range names {
		if (path == name || strings.HasPrefix(path, name+".")) && len(name) > len(best) {
			best = name
		}
	}
	return best, best != ""
}
