package coordinator

import (
	"sort"

	"github.com/astralp2p/astral-go/api/services"
)

// Visibility decides which offerings reach a stream's caller. The node
// computes it before enrolling the stream, because authorization never runs
// under the coordinator lock.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#visibility
type Visibility struct {
	SwarmCaller bool            // the caller sees open offerings
	Permitted   map[string]bool // services the caller holds a discovery permit for on this node
}

// Allows reports whether the caller sees an offering of service whose
// provider is open or private.
func (v *Visibility) Allows(service string, open bool) bool {
	return (open && v.SwarmCaller) || v.Permitted[service]
}

// isOpen reports the effective visibility of b's service on this node: the
// binding's own flag, unless the private list covers it.
func (c *Coordinator) isOpen(b *Binding, service string) bool {
	return b.open && !c.private.Covers(b.provider, service)
}

// isOpenKey is isOpen for an offering key. An offering no live binding owns
// counts as open; its removal is already queued.
func (c *Coordinator) isOpenKey(k offeringKey) bool {
	b := c.owners[k]
	return b == nil || c.isOpen(b, k.name)
}

// Names lists every service a live binding advertises, sorted.
func (c *Coordinator) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.byName))
	for name := range c.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetPrivate replaces the node's private list and applies the change to every
// follow stream of an affected service.
//
// Spec: .ai/system/protocols/services/types/mod.services.private_list.md
func (c *Coordinator) SetPrivate(list *services.PrivateList) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.private
	c.private = list
	for name, bindings := range c.byName {
		for b := range bindings {
			was := b.open && !old.Covers(b.provider, name)
			if now := c.isOpen(b, name); now != was {
				c.applyVisibilityChange(b, name, now)
			}
		}
	}
}

// applyVisibilityChange updates the follows of service after b's binding of it
// became open or private. A follow that may no longer see the offering loses
// it; a follow that may now see it, or a tagged one, re-evaluates it.
func (c *Coordinator) applyVisibilityChange(b *Binding, service string, open bool) {
	k := keyOf(b.provider, service)
	for _, streams := range c.byCaller {
		for st := range streams {
			if st.closed || !st.follow || !st.requests(service) {
				continue
			}
			if st.filter != nil {
				if !st.filter.Allows(service, open) {
					st.hide(k)
					continue
				}
				if st.filter.Allows(service, !open) {
					continue // why: the caller saw it either way
				}
			}
			sl := b.slot(st.caller)
			sl.reevaluate[service] = struct{}{}
			b.schedule(sl)
		}
	}
}
