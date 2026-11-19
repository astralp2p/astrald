package services

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// callerView decides which offerings a discovery's caller sees: open ones
// when the caller is a swarm caller, and any one of a service the caller holds
// a discovery permit for on the provider's node. refresh answers every
// question again after the caller's authority changed.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#visibility
type callerView struct {
	mod    *Module
	caller *astral.Identity
	swarm  atomic.Bool

	mu      sync.Mutex
	permits map[permitKey]bool // authorization results, asked once each until refresh

	// refreshMu orders refresh against member views being applied: a view
	// decided on old answers is applied before refresh starts, so the re-check
	// that follows sees it.
	refreshMu sync.RWMutex
}

type permitKey struct{ service, node string }

func (mod *Module) newCallerView(ctx *astral.Context, caller *astral.Identity) *callerView {
	v := &callerView{mod: mod, caller: caller, permits: map[permitKey]bool{}}
	v.swarm.Store(mod.isSwarmCaller(ctx, caller))
	return v
}

// refresh answers the swarm caller question and every permit question asked
// so far again, and reports whether the caller now sees something it did not.
func (v *callerView) refresh(ctx *astral.Context) (widened bool) {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	swarm := v.mod.isSwarmCaller(ctx, v.caller)
	widened = swarm && !v.swarm.Load()
	v.swarm.Store(swarm)

	v.mu.Lock()
	old := v.permits
	v.mu.Unlock()
	permits := make(map[permitKey]bool, len(old))
	for k, was := range old {
		node, err := astral.ParseIdentity(k.node)
		if err != nil {
			continue
		}
		now := v.mod.Auth.Authorize(ctx, &services.ServiceDiscoveryAction{
			Action:  auth.NewAction(v.caller),
			Service: astral.String8(k.service),
			NodeID:  node,
		})
		permits[k] = now
		widened = widened || (now && !was)
	}
	v.mu.Lock()
	v.permits = permits
	v.mu.Unlock()
	return widened
}

// offer applies one view a swarm member tagged open or private to r, as far as
// the caller sees it.
func (v *callerView) offer(ctx *astral.Context, r *coordinator.Contribution, u *services.Update, open bool) {
	v.refreshMu.RLock()
	defer v.refreshMu.RUnlock()
	r.Update(u, open, v.sees(ctx, string(u.Name), r.Member(), open))
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
	return (open && v.swarm.Load()) || v.permitted(ctx, service, node)
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
	f := &coordinator.Visibility{SwarmCaller: v.swarm.Load(), Permitted: map[string]bool{}}
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
