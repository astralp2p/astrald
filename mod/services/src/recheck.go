package services

import (
	"slices"
	"sync"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// An authority change re-checks the open follows and advertise bindings of the
// identities it affects. Change callbacks only schedule; one goroutine runs
// the re-checks, outside the coordinator lock.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#authority-changes

// openDiscovery is an admitted follow and the stream it feeds.
type openDiscovery struct {
	d  *discovery
	st *coordinator.Stream
}

// authorityWatch holds the open follows by caller, the advertise bindings by
// provider, and the identities waiting for a re-check.
type authorityWatch struct {
	mu       sync.Mutex
	open     map[string]map[*openDiscovery]struct{}
	bindings map[string]map[*coordinator.Binding]struct{}
	pending  map[string]*astral.Identity
	all      bool   // every open follow and binding waits for a re-check
	changes  uint64 // counts scheduled changes; a follow admitted across one re-checks itself
	wake     chan struct{}
}

// init creates the maps of a zero watch. The caller holds mu.
func (w *authorityWatch) init() {
	if w.wake == nil {
		w.open = map[string]map[*openDiscovery]struct{}{}
		w.bindings = map[string]map[*coordinator.Binding]struct{}{}
		w.pending = map[string]*astral.Identity{}
		w.wake = make(chan struct{}, 1)
	}
}

// changeCount reports how many changes were scheduled so far.
func (w *authorityWatch) changeCount() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.changes
}

// addOpen registers an open follow under its caller.
func (w *authorityWatch) addOpen(od *openDiscovery) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	addTo(w.open, od.d.request.Caller.String(), od)
}

func (w *authorityWatch) removeOpen(od *openDiscovery) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	removeFrom(w.open, od.d.request.Caller.String(), od)
}

// addBinding registers an advertise binding under its provider.
func (w *authorityWatch) addBinding(b *coordinator.Binding) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	addTo(w.bindings, b.Provider().String(), b)
}

func (w *authorityWatch) removeBinding(b *coordinator.Binding) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	removeFrom(w.bindings, b.Provider().String(), b)
}

// schedule queues a re-check of id. It never blocks.
func (w *authorityWatch) schedule(id *astral.Identity) {
	if id == nil || id.IsZero() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	w.changes++
	w.pending[id.String()] = id
	w.signal()
}

// scheduleAll queues a re-check of every open follow and binding. It never
// blocks.
func (w *authorityWatch) scheduleAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	w.changes++
	w.all = true
	w.signal()
}

// signal wakes the re-check goroutine without blocking. The caller holds mu.
func (w *authorityWatch) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// take returns the identities waiting for a re-check and clears the queue.
func (w *authorityWatch) take() []*astral.Identity {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	if w.all {
		for k, od := range w.open {
			for o := range od {
				w.pending[k] = o.d.request.Caller
				break
			}
		}
		for k, bs := range w.bindings {
			for b := range bs {
				w.pending[k] = b.Provider()
				break
			}
		}
		w.all = false
	}
	ids := make([]*astral.Identity, 0, len(w.pending))
	for _, id := range w.pending {
		ids = append(ids, id)
	}
	clear(w.pending)
	return ids
}

// follows lists the open follows of caller.
func (w *authorityWatch) follows(caller *astral.Identity) []*openDiscovery {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	var list []*openDiscovery
	for od := range w.open[caller.String()] {
		list = append(list, od)
	}
	return list
}

// bindingsOf lists the advertise bindings of provider.
func (w *authorityWatch) bindingsOf(provider *astral.Identity) []*coordinator.Binding {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	var list []*coordinator.Binding
	for b := range w.bindings[provider.String()] {
		list = append(list, b)
	}
	return list
}

// carriedBy lists the open follows a swarm member carries to this node.
func (w *authorityWatch) carriedBy(carrier *astral.Identity) []*openDiscovery {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	var list []*openDiscovery
	for _, set := range w.open {
		for od := range set {
			if od.d.carrier != nil && od.d.carrier.IsEqual(carrier) {
				list = append(list, od)
			}
		}
	}
	return list
}

// wakeChan is closed over by the re-check goroutine.
func (w *authorityWatch) wakeChan() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.init()
	return w.wake
}

func addTo[T comparable](m map[string]map[T]struct{}, k string, v T) {
	if m[k] == nil {
		m[k] = map[T]struct{}{}
	}
	m[k][v] = struct{}{}
}

func removeFrom[T comparable](m map[string]map[T]struct{}, k string, v T) {
	delete(m[k], v)
	if len(m[k]) == 0 {
		delete(m, k)
	}
}

// watchAuthority registers the change callbacks that schedule re-checks.
func (mod *Module) watchAuthority() {
	// why: the withdrawn contracts' subjects are not reported, and withdrawals
	// are rare operator actions, so every follow is re-checked.
	mod.Auth.OnContractWithdrawn(func(*astral.Identity) { mod.authority.scheduleAll() })
	mod.Auth.OnContractIndexed(func(sc *auth.SignedContract) {
		if sc == nil || sc.Contract == nil {
			return
		}
		mod.authority.schedule(sc.Issuer)
		mod.authority.schedule(sc.Subject)
	})
}

// runRechecks re-checks the scheduled identities until ctx ends.
func (mod *Module) runRechecks(ctx *astral.Context) {
	wakeCh := mod.authority.wakeChan()
	for {
		for _, id := range mod.authority.take() {
			mod.recheck(ctx, id)
		}
		select {
		case <-ctx.Done():
			return
		case <-wakeCh:
		}
	}
}

// recheck applies id's current authority to its open follows, to the follows
// it carries to this node, and to its advertise bindings.
func (mod *Module) recheck(ctx *astral.Context, id *astral.Identity) {
	for _, od := range mod.authority.follows(id) {
		if od.d.carrier != nil {
			mod.recheckCarry(ctx, od)
			continue
		}
		mod.recheckFollow(ctx, od)
	}
	// why: a carried follow is registered under its app, but the carrying
	// node's own relay right and discovery permission also decide it.
	for _, od := range mod.authority.carriedBy(id) {
		mod.recheckCarry(ctx, od)
	}
	bindings := mod.authority.bindingsOf(id)
	if len(bindings) == 0 {
		return
	}
	if mod.Auth.Authorize(ctx, &auth.ServeAppsAction{Action: auth.NewAction(id)}) {
		return
	}
	mod.log.Logv(1, "%v lost serve apps; closing its %v bindings", id, len(bindings))
	for _, b := range bindings {
		b.Close()
	}
}

// recheckCarry closes a follow a swarm member carries to this node once the
// member may no longer carry it.
func (mod *Module) recheckCarry(ctx *astral.Context, od *openDiscovery) {
	if !mod.carryAllowed(ctx, od.d) {
		od.st.Close()
	}
}

// recheckFollow refreshes a local follow's view, then withholds what the caller
// may no longer see and reveals what it may now see.
func (mod *Module) recheckFollow(ctx *astral.Context, od *openDiscovery) {
	d := od.d
	widened := d.view.refresh(ctx)
	mod.coord.SetFilter(od.st, d.view.localFilter(ctx, d.request.Names), mod.changedNotGranted(ctx, d))
	for _, r := range od.st.Contributions() {
		var gone []coordinator.Offering
		for _, o := range r.Offered() {
			if !d.view.sees(ctx, o.Name, r.Member(), o.Open) {
				gone = append(gone, o)
			}
		}
		r.Hide(gone)
	}
	if widened && d.request.Swarm {
		mod.coord.RestartContributions(od.st)
	}
}

// changedNotGranted returns the follow's current not_granted list when it
// differs from the last one sent, and nil otherwise.
func (mod *Module) changedNotGranted(ctx *astral.Context, d *discovery) *services.NotGranted {
	if d.request.All {
		return nil
	}
	nodeIDs := []*astral.Identity{mod.node.Identity()}
	if d.request.Swarm {
		nodeIDs = append(nodeIDs, mod.swarmMembers()...)
	}
	list := d.view.notGranted(ctx, d.request.Names, nodeIDs)
	if slices.Equal(list, d.notGranted) {
		return nil
	}
	d.notGranted = list
	return notGranted(list)
}

// carryAllowed reports whether the node carrying d to this node may still do
// so: it holds the relay right for the app and is a member of the local swarm
// (see admitCarry).
func (mod *Module) carryAllowed(ctx *astral.Context, d *discovery) bool {
	if !mod.Auth.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(d.carrier), ForID: d.request.Caller}) {
		return false
	}
	return slices.ContainsFunc(mod.swarmMembers(), d.carrier.IsEqual)
}

// closeCarriesOf closes the follows a node carries to this node once it is no
// longer a member of the local swarm.
func (mod *Module) closeCarriesOf(node *astral.Identity) {
	if node == nil || slices.ContainsFunc(mod.swarmMembers(), node.IsEqual) {
		return
	}
	for _, od := range mod.authority.carriedBy(node) {
		od.st.Close()
	}
}
