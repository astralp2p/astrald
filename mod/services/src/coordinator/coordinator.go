// Package coordinator holds the node-side state of the Services API. It
// performs no I/O; the transports and sinks its callers supply do.
//
// A Binding is one provider's hold on a fixed set of service names: an app's
// advertise channel or a native registration under the node identity. The
// node asks a binding to evaluate one service for one caller at a time.
// A Stream is one consumer's discovery. Its attempt is the initial attempt,
// which ends at the boundary: services.incomplete when needed, then eos.
// A Contribution is one swarm member's part of a stream.
//
// One mutex in Coordinator guards all of this state.
//
// Spec: .ai/system/protocols/services/README.md
// Design: https://wiki.satforge.dev/doc/services-api-implementation-plan-zah9USQT2q
package coordinator

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

var (
	// ErrOwned rejects a registration naming an offering a live binding owns.
	ErrOwned = errors.New("service already advertised by this provider")
	// ErrMalformed fails a binding that sent an answer or change the node
	// cannot accept.
	ErrMalformed = errors.New("malformed provider message")
)

// Config sets the coordinator's timing.
type Config struct {
	InitialBudget  time.Duration // how long an initial attempt waits for answers
	RequestTimeout time.Duration // how long one ask waits for its answer
	Clock          Clock
}

// DefaultConfig holds the provisional timings.
// note: provisional values; see the design link in the package doc.
func DefaultConfig() Config {
	return Config{InitialBudget: 5 * time.Second, RequestTimeout: 10 * time.Second, Clock: SystemClock{}}
}

// Coordinator is the single synchronization point of the Services module.
//
// why: one mutex over every structure. Atomic ownership, gap-free enrollment
// and initial accounting are one critical section each; splitting the state
// across locks would reintroduce the races those sections exist to prevent.
// note: no channel I/O, authorization or evaluation runs under mu.
type Coordinator struct {
	mu       sync.Mutex
	cfg      Config
	owners   map[offeringKey]*Binding
	byName   map[string]map[*Binding]struct{}
	byCaller map[string]map[*Stream]struct{}
}

func New(cfg Config) *Coordinator {
	if cfg.Clock == nil {
		cfg.Clock = SystemClock{}
	}
	return &Coordinator{
		cfg:      cfg,
		owners:   map[offeringKey]*Binding{},
		byName:   map[string]map[*Binding]struct{}{},
		byCaller: map[string]map[*Stream]struct{}{},
	}
}

type offeringKey struct {
	provider string
	name     string
}

// callerKey is the map key of a caller in byCaller and in a binding's slots.
func callerKey(id *astral.Identity) string {
	return id.String()
}

func keyOf(provider *astral.Identity, name string) offeringKey {
	return offeringKey{provider: provider.String(), name: name}
}

// Transport carries asks to one provider.
type Transport interface {
	// Send delivers one ask. It runs on the binding's sender goroutine,
	// outside the coordinator lock. An error closes the binding.
	Send(*services.Ask) error
	// Close ends the transport. It is called once, after the binding closes.
	Close()
}

// Register claims every (provider, name) pair for a new binding, or none.
func (c *Coordinator) Register(provider *astral.Identity, names []string, t Transport) (*Binding, error) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	c.mu.Lock()
	for _, name := range sorted {
		if _, owned := c.owners[keyOf(provider, name)]; owned {
			c.mu.Unlock()
			return nil, ErrOwned
		}
	}

	b := newBinding(c, provider, sorted, t)
	for _, name := range sorted {
		c.owners[keyOf(provider, name)] = b
		if c.byName[name] == nil {
			c.byName[name] = map[*Binding]struct{}{}
		}
		c.byName[name][b] = struct{}{}
	}
	c.reevaluateForFollowers(b)
	c.mu.Unlock()

	go b.sendLoop()
	return b, nil
}

// reevaluateForFollowers asks a new binding to evaluate its names for every
// follow stream that requests one of them.
func (c *Coordinator) reevaluateForFollowers(b *Binding) {
	for _, streams := range c.byCaller {
		for st := range streams {
			if !st.follow {
				continue
			}
			for _, name := range b.names {
				if _, ok := st.names[name]; ok {
					sl := b.slot(st.caller)
					sl.reevaluate[name] = struct{}{}
					b.schedule(sl)
				}
			}
		}
	}
}

// Discover enrolls a stream and sets up its initial attempt in one critical
// section. Every local binding of a requested name and every member owes one
// initial evaluation per name. The caller has authorized every name on every
// member beforehand.
func (c *Coordinator) Discover(caller *astral.Identity, names []string, follow bool, members []*astral.Identity) *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.enrollStream(caller, names, follow)
	at := newAttempt(st)
	st.attempt = at
	touched := c.addLocalEvaluations(at, caller, names)
	addMemberEvaluations(at, members, names)
	c.startBudget(at)
	for _, sl := range touched {
		sl.binding.schedule(sl)
	}
	if len(at.unresolved) == 0 {
		at.finish()
	}
	return st
}

// enrollStream registers a new stream under its caller, so later answers and
// changes for that caller reach it.
func (c *Coordinator) enrollStream(caller *astral.Identity, names []string, follow bool) *Stream {
	st := newStream(c, caller, names, follow)
	ck := callerKey(caller)
	if c.byCaller[ck] == nil {
		c.byCaller[ck] = map[*Stream]struct{}{}
	}
	c.byCaller[ck][st] = struct{}{}
	return st
}

// addLocalEvaluations queues one initial evaluation per local binding of each
// requested name, and returns the slots that may now ask.
func (c *Coordinator) addLocalEvaluations(at *attempt, caller *astral.Identity, names []string) []*askSlot {
	var touched []*askSlot
	for _, name := range names {
		for b := range c.byName[name] {
			sl := b.slot(caller)
			o := &evaluation{at: at, binding: b, slot: sl, service: name}
			at.unresolved[o] = struct{}{}
			sl.waiting[name] = append(sl.waiting[name], o)
			touched = append(touched, sl)
		}
	}
	return touched
}

// addMemberEvaluations adds one contribution per swarm member, each owing one
// initial evaluation per requested name.
func addMemberEvaluations(at *attempt, members []*astral.Identity, names []string) {
	st := at.st
	for _, member := range members {
		r := newContribution(st, member)
		for _, name := range names {
			o := &evaluation{at: at, contribution: r, service: name}
			at.unresolved[o] = struct{}{}
			r.unresolved[name] = o
		}
		st.contributions = append(st.contributions, r)
	}
}

// startBudget bounds the initial attempt by the initial budget.
func (c *Coordinator) startBudget(at *attempt) {
	at.budget = c.cfg.Clock.AfterFunc(c.cfg.InitialBudget, func() { c.budgetExpired(at) })
}

// follows reports whether caller has an open follow stream requesting name.
func (c *Coordinator) follows(caller *astral.Identity, name string) bool {
	for st := range c.byCaller[callerKey(caller)] {
		if _, ok := st.names[name]; ok && st.follow && !st.closed {
			return true
		}
	}
	return false
}
