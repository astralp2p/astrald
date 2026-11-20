package nodes

import (
	"context"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

type linkWatcher struct {
	match func(*Link, *string) bool
	ch    chan *Link
	// keeps marks a caller that asked for a new link; a strategy link it takes is never excess.
	keeps bool
}

type LinkPool struct {
	closed   atomic.Bool
	mod      *Module
	mu       sync.Mutex
	links    sig.Set[*Link]
	watchers sig.Set[*linkWatcher]
	linkers  sig.Map[string, *NodeLinker]
}

func NewLinkPool(mod *Module) *LinkPool {
	return &LinkPool{
		mod: mod,
	}
}

// AddLink registers link, wires its mux router, emits a created event, closes the prunable links it outranks, probes the older links to the same peer, and spawns a goroutine that removes it and closes its sessions when it dies.
func (pool *LinkPool) AddLink(link *Link) error {
	dir := "in"
	netName := "unknown network"

	if link.outbound {
		dir = "out"
	}

	switch {
	case link.LocalEndpoint() != nil:
		netName = link.LocalEndpoint().Network()
	case link.RemoteEndpoint() != nil:
		netName = link.RemoteEndpoint().Network()
	}

	linkCount, err := pool.addCounted(link)
	if err != nil {
		return err
	}

	// why: a handshake can finish while module shutdown closes the pool.
	if pool.closed.Load() {
		pool.links.Remove(link)
		link.CloseWithError(context.Canceled)
		return context.Canceled
	}

	link.GetMux().SetRouter(pool.mod.node)

	pool.mod.log.Infov(1, "added %v-link with %v (%v)", dir, link.RemoteIdentity(), netName)

	if !link.outbound {
		pool.notifyLinkWatchers(link, nil)
	}

	pool.mod.Events.Emit(&nodes.LinkCreatedEvent{
		RemoteIdentity: link.RemoteIdentity(),
		LinkID:         link.id,
		LinkCount:      astral.Uint32(linkCount),
	})

	if linkCount > 1 {
		go pool.closeOutranked(link)
		pool.probeOlderLinks(link)
	}

	go func() {
		<-link.Done()

		pool.links.Remove(link)
		link.GetMux().closeAllSessions()

		remaining := pool.links.Select(func(v *Link) bool {
			return v.RemoteIdentity().IsEqual(link.RemoteIdentity())
		})

		pool.mod.Events.Emit(&nodes.LinkClosedEvent{
			RemoteIdentity: link.RemoteIdentity(),
			Forced:         false,
			LinkCount:      astral.Uint8(len(remaining)),
		})

		pool.mod.log.Info("closed %v-link with %v (%v): %v", dir, link.RemoteIdentity(), netName, link.Err())
	}()

	go pool.mod.reflectLink(link)

	return nil
}

// probeOlderLinks wakes the ping loop of every other pooled link to the remote
// identity of link, so each sends one ping.
// why: a new link to a peer that already has links often means the peer moved
// and an older link is dead; the ping closes a dead one within its ping timeout,
// and the LinkClosedEvent it leaves starts the reconnect sync over a survivor.
// note: a healthy older link answers the ping and stays.
func (pool *LinkPool) probeOlderLinks(link *Link) {
	older := pool.links.Select(func(v *Link) bool {
		return v != link && v.RemoteIdentity().IsEqual(link.RemoteIdentity())
	})
	for _, old := range older {
		old.Wake()
	}
}

// Close ends the pool lifetime and disconnects every peer.
// why: embedded nodes stop without exiting the process or closing its sockets.
func (pool *LinkPool) Close() {
	if !pool.closed.CompareAndSwap(false, true) {
		return
	}
	for _, link := range pool.links.Clone() {
		link.CloseWithError(context.Canceled)
	}
}

// addCounted adds link to the pool and answers how many links to its remote
// identity the pool holds with it.
// why one lock over both: of links to one node added at once, exactly one then
// counts 1, and the user module starts its first-link sync on that count alone.
func (pool *LinkPool) addCounted(link *Link) (int, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if err := pool.links.Add(link); err != nil {
		return 0, err
	}

	return len(pool.links.Select(func(v *Link) bool {
		return v.RemoteIdentity().IsEqual(link.RemoteIdentity())
	})), nil
}

// SelectLinkWith returns a link to id, preferring a non-high-pressure link.
func (pool *LinkPool) SelectLinkWith(id *astral.Identity) *Link {
	if pool.closed.Load() {
		return nil
	}
	var fallback *Link

	for _, link := range pool.links.Clone() {
		if link.Err() != nil || !link.RemoteIdentity().IsEqual(id) {
			continue
		}

		if !link.PressureHigh() {
			return link
		}

		if fallback == nil {
			fallback = link
		}
	}

	return fallback
}

type LinkResult struct {
	Link *Link
	Err  error
}

func (pool *LinkPool) subscribe(match func(*Link, *string) bool, keeps bool) *linkWatcher {
	w := &linkWatcher{
		match: match,
		ch:    make(chan *Link, 1),
		keeps: keeps,
	}

	pool.watchers.Add(w)
	return w
}

func (pool *LinkPool) unsubscribe(w *linkWatcher) {
	pool.watchers.Remove(w)
}

func (pool *LinkPool) notifyLinkWatchers(s *Link, strategy *string) bool {
	return pool.notifyWatchers(pool.watchers.Clone(), s, strategy)
}

// watchersKeeping returns the watchers whose keeps flag equals keeps.
func (pool *LinkPool) watchersKeeping(keeps bool) []*linkWatcher {
	return pool.watchers.Select(func(w *linkWatcher) bool { return w.keeps == keeps })
}

// notifyWatchers offers s to every watcher of ws that matches it and answers whether one took it.
func (pool *LinkPool) notifyWatchers(ws []*linkWatcher, s *Link, strategy *string) bool {
	used := false
	for _, w := range ws {
		if !w.match(s, strategy) {
			continue
		}

		select {
		case w.ch <- s:
			used = true
		default:
		}
	}

	return used
}

func (pool *LinkPool) getOrCreateNodeLinker(target *astral.Identity) *NodeLinker {
	key := target.String()

	if linker, ok := pool.linkers.Get(key); ok {
		return linker
	}

	linker := NewNodeLinker(pool.mod, target)
	if existing, ok := pool.linkers.Set(key, linker); !ok {
		return existing
	}

	return linker
}

// RetrieveLink returns a matching existing link immediately unless ForceNew is set; otherwise it activates linking strategies and delivers the first link that matches, ctx cancellation, or ErrLinkNotProduced.
func (pool *LinkPool) RetrieveLink(
	ctx *astral.Context,
	target *astral.Identity,
	opts ...RetrieveLinkOption,
) <-chan LinkResult {

	var o RetrieveLinkOptions
	for _, opt := range opts {
		opt(&o)
	}

	match := func(s *Link, strategy *string) bool {
		if !s.RemoteIdentity().IsEqual(target) {
			return false
		}
		if strategy != nil && len(o.Strategies) > 0 {
			return slices.Contains(o.Strategies, *strategy)
		}

		if len(o.Networks) > 0 {
			return slices.Contains(o.Networks, s.Network())
		}
		return true
	}

	if !o.ForceNew {
		links := pool.links.Select(func(s *Link) bool { return match(s, nil) })
		if len(links) > 0 {
			return sig.ArrayToChan([]LinkResult{{Link: links[0]}})
		}
	}

	result := make(chan LinkResult, 1)

	go func() {
		defer close(result)

		strategyCtx, cancelStrategies := ctx.WithCancel()
		defer cancelStrategies()

		// why: a forced or network-bound request asks for a link the pool's
		// existing links do not satisfy, so its link is not a duplicate.
		w := pool.subscribe(match, o.ForceNew || len(o.Networks) > 0)
		defer pool.unsubscribe(w)

		linker := pool.getOrCreateNodeLinker(target)
		done := linker.Activate(strategyCtx, o.Strategies)

		select {
		case <-done:
			select {
			case s := <-w.ch:
				result <- LinkResult{Link: s}
			default:
				result <- LinkResult{Err: nodesmod.ErrLinkNotProduced}
			}
		case <-ctx.Done():
			result <- LinkResult{Err: ctx.Err()}
		case s := <-w.ch:
			result <- LinkResult{Link: s}
		}
	}()

	return result
}

// RetrieveLinkOptions controls how RetrieveLink behaves.
type RetrieveLinkOptions struct {
	ForceNew   bool
	Strategies []string
	Networks   []string
}

// RetrieveLinkOption is a functional option for RetrieveLink.
type RetrieveLinkOption func(*RetrieveLinkOptions)

func WithForceNew() RetrieveLinkOption {
	return func(o *RetrieveLinkOptions) {
		o.ForceNew = true
	}
}

func WithStrategies(strategies ...string) RetrieveLinkOption {
	return func(o *RetrieveLinkOptions) {
		o.Strategies = append(o.Strategies, strategies...)
	}
}

func WithNetworks(networks ...string) RetrieveLinkOption {
	return func(o *RetrieveLinkOptions) {
		o.Networks = append(o.Networks, networks...)
	}
}
