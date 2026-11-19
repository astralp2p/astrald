package nodes

import (
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

const (
	// pullTimeout bounds one ask of the linked siblings for a target's relay contracts.
	pullTimeout = 1 * time.Second
	// pullMissTTL is how long a target whose ask yielded no relay is not asked for again.
	pullMissTTL = 30 * time.Second
)

// relayPull is the state of the routing-time pull of missing relay contracts:
// the asks running, one per target, and the targets whose last ask yielded no
// relay, until when they are not asked for again.
// Spec: .ai/system/topics/app-routing.md, Resolving the host.
type relayPull struct {
	flights sig.Map[string, *pullFlight]
	misses  sig.Map[string, time.Time]
	// now is the clock of the miss cache; nil is time.Now.
	now func() time.Time
}

// pullFlight is one ask for a target, shared by every miss for it while it runs.
type pullFlight struct {
	done   chan struct{}
	relays []*astral.Identity
}

// pullRelays asks the linked siblings for target's relay contracts when this
// node holds none, indexes the first one admitted, and returns the relays the
// index then names for target. It returns nil at once for a gated target.
//
// why the gate: the ask's own queries target siblings, which the gate
// excludes, so an ask never starts another ask; a recent miss fails fast.
// why a detached context: the ask is shared, so one caller ending early must
// not cut it short for the others or record a false miss.
func (mod *Module) pullRelays(ctx *astral.Context, target *astral.Identity) []*astral.Identity {
	if mod.isPullGated(target) {
		return nil
	}

	flight := &pullFlight{done: make(chan struct{})}
	if running, started := mod.pull.flights.Set(target.String(), flight); !started {
		flight = running
	} else {
		pctx, cancel := ctx.Detach().WithTimeout(pullTimeout)
		go func() {
			defer cancel()
			flight.relays = mod.runPull(pctx, target)
			mod.pull.flights.Delete(target.String())
			close(flight.done)
		}()
	}

	select {
	case <-flight.done:
		return flight.relays
	case <-ctx.Done():
		return nil
	}
}

// isPullGated reports whether target is never asked for: this node, a member
// of the local swarm, the user, or a target with an unexpired miss.
func (mod *Module) isPullGated(target *astral.Identity) bool {
	if target.IsEqual(mod.node.Identity()) || target.IsEqual(mod.User.Identity()) {
		return true
	}
	if slices.ContainsFunc(mod.User.LocalSwarm(), target.IsEqual) {
		return true
	}

	until, ok := mod.pull.misses.Get(target.String())
	if !ok {
		return false
	}
	if mod.pull.clock().Before(until) {
		return true
	}
	mod.pull.misses.Delete(target.String())
	return false
}

// runPull runs one ask for target: it indexes the first contract a sibling
// delivers that the index admits, and returns the relays the index then names.
// An ask that leaves no relay records a miss.
func (mod *Module) runPull(ctx *astral.Context, target *astral.Identity) []*astral.Identity {
	mod.log.Logv(1, "asking linked siblings for the relay contracts of %v", target)

	for sender, sc := range mod.User.FetchRelayContracts(ctx, target) {
		if err := mod.Auth.IndexContract(ctx, sc); err != nil {
			mod.log.Logv(1, "indexing the relay contract of %v from %v: %v", target, sender, err)
			continue
		}

		// why re-read the index: IndexContract answers nil for a contract the
		// index already holds, a locally withdrawn one among them.
		if relays := mod.heldRelays(ctx, target); len(relays) > 0 {
			mod.log.Logv(1, "pulled the relay contract of %v from %v", target, sender)
			return relays
		}
	}

	if relays := mod.heldRelays(ctx, target); len(relays) > 0 {
		return relays
	}

	mod.pull.addMiss(target)
	mod.log.Logv(1, "no relay contract of %v from linked siblings; not asking again for %v", target, pullMissTTL.String())
	return nil
}

// heldRelays returns the subjects of the active relay contracts target issued
// that the index holds, this node excluded.
func (mod *Module) heldRelays(ctx *astral.Context, target *astral.Identity) (relays []*astral.Identity) {
	contracts, err := mod.Auth.SignedContracts().
		WithIssuer(target).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx.ExcludeZone(astral.ZoneNetwork))
	if err != nil {
		mod.log.Errorv(1, "reading the relay contracts of %v: %v", target, err)
		return nil
	}

	for _, sc := range contracts {
		if !sc.Subject.IsEqual(mod.node.Identity()) {
			relays = append(relays, sc.Subject)
		}
	}
	return relays
}

// addMiss records that target is not asked for until pullMissTTL from now, and
// drops the misses that have expired.
func (p *relayPull) addMiss(target *astral.Identity) {
	now := p.clock()
	for key, until := range p.misses.Clone() {
		if !now.Before(until) {
			p.misses.Delete(key)
		}
	}
	p.misses.Replace(target.String(), now.Add(pullMissTTL))
}

func (p *relayPull) clock() time.Time {
	if p.now == nil {
		return time.Now()
	}
	return p.now()
}
