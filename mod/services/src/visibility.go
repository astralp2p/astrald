package services

import (
	"slices"
	"sort"
	"sync"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// callerView decides which offerings a discovery's caller sees: open ones
// when the caller is a swarm caller, and any one of a service the caller holds
// a discovery permit for on the provider's node.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#visibility
type callerView struct {
	mod    *Module
	caller *astral.Identity
	swarm  bool

	mu      sync.Mutex
	permits map[permitKey]bool // authorization results, asked once each
}

type permitKey struct{ service, node string }

func (mod *Module) newCallerView(ctx *astral.Context, caller *astral.Identity) *callerView {
	return &callerView{mod: mod, caller: caller, swarm: mod.isSwarmCaller(ctx, caller), permits: map[permitKey]bool{}}
}

// isSwarmCaller reports whether caller sees open offerings: an app hosted on a
// swarm node, the user, or a swarm node.
func (mod *Module) isSwarmCaller(ctx *astral.Context, caller *astral.Identity) bool {
	switch {
	case caller == nil || caller.IsZero():
		return false
	case caller.IsEqual(mod.node.Identity()):
		// why: the router runs a caller-less local query as the node.
		return false
	case isUser(mod.User.Identity(), caller):
		return true
	case slices.ContainsFunc(mod.User.LocalSwarm(), caller.IsEqual):
		return true
	}
	return mod.Apphost.IsSwarmApp(ctx, caller)
}

// sees reports whether the caller sees an offering of service on node whose
// provider is open or private there.
func (v *callerView) sees(ctx *astral.Context, service string, node *astral.Identity, open bool) bool {
	return (open && v.swarm) || v.permitted(ctx, service, node)
}

// permitted reports whether the caller holds a discovery permit for service on
// node.
func (v *callerView) permitted(ctx *astral.Context, service string, node *astral.Identity) bool {
	k := permitKey{service, node.String()}
	v.mu.Lock()
	ok, known := v.permits[k]
	v.mu.Unlock()
	if known {
		return ok
	}
	ok = v.mod.Auth.Authorize(ctx, &services.ServiceDiscoveryAction{
		Action:  auth.NewAction(v.caller),
		Service: astral.String8(service),
		NodeID:  node,
	})
	v.mu.Lock()
	v.permits[k] = ok
	v.mu.Unlock()
	return ok
}

// localFilter is the coordinator's filter for offerings of this node's
// bindings of names.
func (v *callerView) localFilter(ctx *astral.Context, names []string) *coordinator.Visibility {
	f := &coordinator.Visibility{SwarmCaller: v.swarm, Permitted: map[string]bool{}}
	for _, name := range names {
		f.Permitted[name] = v.permitted(ctx, name, v.mod.node.Identity())
	}
	return f
}

// notGranted lists the names the caller holds no discovery permit for on at
// least one of nodes, sorted.
func (v *callerView) notGranted(ctx *astral.Context, names []string, nodes []*astral.Identity) []string {
	var list []string
	for _, name := range names {
		for _, node := range nodes {
			if !v.permitted(ctx, name, node) {
				list = append(list, name)
				break
			}
		}
	}
	sort.Strings(list)
	return list
}

// isUser reports whether caller is the node's user; a node without one has no
// user.
func isUser(user, caller *astral.Identity) bool {
	return user != nil && !user.IsZero() && caller.IsEqual(user)
}
