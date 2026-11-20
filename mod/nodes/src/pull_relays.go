package nodes

import (
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

const (
	// pullMissTTL is how long a target whose complete ask yielded no relay is not asked for again.
	pullMissTTL = 30 * time.Second
	// pullIndexTimeout bounds one index write or read of a pulled contract.
	pullIndexTimeout = 2 * time.Second
)

// The pull has two budgets, package variables so tests can shrink them.
var (
	// pullFlightTimeout bounds the detached ask of the linked siblings for a
	// target's relay contracts. An answer that lands inside it is indexed even
	// when the caller has left.
	pullFlightTimeout = 10 * time.Second
	// pullWait is how long a caller waits for the flight before it routes on.
	pullWait = 3 * time.Second
)

// relayPull is the state of the routing-time pull of missing relay contracts:
// the asks running, one per target, and the targets whose last ask yielded no
// relay, until when they are not asked for again.
// An ask records a miss only when it is complete: every asked sibling answered
// without error before the flight ended. A budget that ran out or a failed ask
// is not a "no".
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
// why two budgets: the flight runs up to pullFlightTimeout so a slow sibling's
// answer is still indexed; a caller waits at most pullWait and routes on, and
// the next query finds the indexed relay.
func (mod *Module) pullRelays(ctx *astral.Context, target *astral.Identity) []*astral.Identity {
	if mod.isPullGated(target) {
		return nil
	}

	flight := &pullFlight{done: make(chan struct{})}
	if running, started := mod.pull.flights.Set(target.String(), flight); !started {
		flight = running
	} else {
		pctx, cancel := ctx.Detach().WithTimeout(pullFlightTimeout)
		go func() {
			defer cancel()
			flight.relays = mod.runPull(pctx, target)
			mod.pull.flights.Delete(target.String())
			close(flight.done)
		}()
	}

	wait := time.NewTimer(pullWait)
	defer wait.Stop()
	select {
	case <-flight.done:
		return flight.relays
	case <-wait.C:
		return nil
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

// runPull runs one ask for target: it returns at once the relays the index
// already names, else indexes the first contract a sibling delivers that the
// index admits, and returns the relays the index then names.
// An ask that leaves no relay records a miss only when it is complete (every
// asked sibling answered without error) and ctx has not ended; otherwise it
// returns nil and records nothing.
//
// why a fresh context for the index: ctx is the flight budget, and a contract
// delivered as it ends must still be indexed and read back.
func (mod *Module) runPull(ctx *astral.Context, target *astral.Identity) []*astral.Identity {
	if relays := mod.heldRelays(ctx, target); len(relays) > 0 {
		return relays
	}

	mod.log.Logv(1, "asking linked siblings for the relay contracts of %v", target)

	contracts, complete := mod.User.FetchRelayContractsReport(ctx, target)
	for sender, sc := range contracts {
		ictx, cancel := ctx.Detach().WithTimeout(pullIndexTimeout)
		err := mod.Auth.IndexContract(ictx, sc)
		if err != nil {
			cancel()
			mod.log.Logv(1, "indexing the relay contract of %v from %v: %v", target, sender, err)
			continue
		}

		// why re-read the index: IndexContract answers nil for a contract the
		// index already holds, a locally withdrawn one among them.
		relays := mod.heldRelays(ictx, target)
		cancel()
		if len(relays) > 0 {
			mod.log.Logv(1, "pulled the relay contract of %v from %v", target, sender)
			return relays
		}
	}

	rctx, cancel := ctx.Detach().WithTimeout(pullIndexTimeout)
	defer cancel()
	if relays := mod.heldRelays(rctx, target); len(relays) > 0 {
		return relays
	}

	if ctx.Err() != nil || !complete() {
		mod.log.Logv(1, "the ask for the relay contracts of %v ran out of time or failed; no miss recorded", target)
		return nil
	}

	mod.pull.addMiss(target)
	mod.log.Logv(1, "no relay contract of %v from linked siblings; not asking again for %v", target, pullMissTTL.String())
	return nil
}

// heldRelays returns the subjects of the active relay contracts target issued
// that the index holds, this node excluded; on a claimed node only current
// members of the local swarm (admitsRelayer).
// Spec: .ai/system/topics/app-routing.md, Resolving the host.
//
// why the member filter: a former member's relay contract stays indexed after
// it leaves, and apphost's candidate filter alone leaves the relay list empty,
// which sends routing here.
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
		if !sc.Subject.IsEqual(mod.node.Identity()) && mod.admitsRelayer(sc.Subject) {
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
