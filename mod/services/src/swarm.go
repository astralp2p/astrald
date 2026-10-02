package services

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// Restoring a lost member: retry on a new link to it, and otherwise after a
// capped backoff, while the follow is open.
// note: provisional values, like the coordinator's.
const (
	carryRetryFirst = time.Second
	carryRetryMax   = time.Minute
)

// carry feeds one swarm member's contribution to a stream: the member's own
// discovery for app, asked by this node naming app in for. With follow it
// restores the contribution after a loss for as long as ctx lasts.
func (mod *Module) carry(ctx *astral.Context, r *coordinator.Remote, app *astral.Identity, names []string, follow bool) {
	delay := carryRetryFirst
	for {
		// why: register for the link event before asking, so a link created
		// while the member is being asked is not missed.
		linked := mod.links.wait(r.Member())
		initial, err := mod.carryOnce(ctx, r, app, names, follow)
		if ctx.Err() != nil {
			return
		}
		if !follow {
			if err != nil && !initial {
				r.Lost()
			}
			return
		}
		mod.log.Logv(2, "swarm discovery on %v ended: %v", r.Member(), err)
		r.Lost()
		if initial {
			delay = carryRetryFirst
		}
		select {
		case <-ctx.Done():
			return
		case <-linked:
		case <-time.After(delay):
		}
		delay = min(delay*2, carryRetryMax)
	}
}

// carryOnce runs one discovery on the member until it ends, and reports
// whether the member's initial outcome arrived.
func (mod *Module) carryOnce(ctx *astral.Context, r *coordinator.Remote, app *astral.Identity, names []string, follow bool) (bool, error) {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)
	mod.pushRelayContract(ctx, app, r.Member())

	client := servicescli.New(r.Member(), core.NewClientAs(mod.node, mod.node.Identity()))
	events, err := client.DiscoverFor(ctx, app, names, follow)
	if err != nil {
		return false, err
	}

	initial := false
	for ev := range events {
		switch {
		case ev.Update != nil:
			r.Offer(ev.Update)
		case ev.Removed != nil:
			r.Remove(ev.Removed)
		case ev.Initial != nil:
			initial = true
			r.Initial(ev.Initial.Incomplete)
		case ev.Err != nil:
			return initial, ev.Err
		}
	}
	return initial, nil
}

// pushRelayContract shows the member that this node represents app, by pushing
// the relay contract app signed with this node. It does nothing when there is
// none, as for the user identity, which members relay for by default.
func (mod *Module) pushRelayContract(ctx *astral.Context, app, member *astral.Identity) {
	contracts, err := mod.Auth.SignedContracts().
		WithIssuer(app).
		WithSubject(mod.node.Identity()).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx)
	if err != nil || len(contracts) == 0 {
		return
	}
	if err := mod.Objects.Push(ctx, member, contracts[0]); err != nil {
		mod.log.Logv(1, "push relay contract of %v to %v: %v", app, member, err)
	}
}

// ReceiveObject watches this node's link events, which restore lost swarm
// contributions.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	e, ok := drop.Object().(*events.Event)
	// why: link events report this node's own links; one sent by another node is forged.
	if !ok || !drop.SenderID().IsEqual(mod.node.Identity()) {
		return nil
	}
	if lc, ok := e.Data.(*nodes.LinkCreatedEvent); ok {
		mod.links.notify(lc.RemoteIdentity)
	}
	return nil
}

// linkWaiters signals goroutines waiting for a link to a node.
type linkWaiters struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

// wait returns a channel closed when a link to id is next created.
func (l *linkWaiters) wait(id *astral.Identity) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]chan struct{}{}
	}
	ch, ok := l.m[id.String()]
	if !ok {
		ch = make(chan struct{})
		l.m[id.String()] = ch
	}
	return ch
}

func (l *linkWaiters) notify(id *astral.Identity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ch, ok := l.m[id.String()]; ok {
		close(ch)
		delete(l.m, id.String())
	}
}
