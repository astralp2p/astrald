// Package coordinator holds the node-side state of the Services API: who owns
// which offering, which caller each provider is evaluating, what each discovery
// stream still owes its consumer. It performs no I/O; transports and sinks
// supplied by the caller do.
//
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
	// ErrOwned rejects a registration naming an offering a live source owns.
	ErrOwned = errors.New("service already advertised by this provider")
	// ErrMalformed fails a source that sent an answer or change the node cannot
	// accept.
	ErrMalformed = errors.New("malformed provider message")
)

// Config sets the coordinator's timing.
type Config struct {
	InitialBudget  time.Duration // how long an initial attempt waits for answers
	RequestTimeout time.Duration // how long one ask waits for its answer
	Clock          Clock
}

// DefaultConfig holds the plan's provisional values.
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

func keyOf(provider *astral.Identity, name string) offeringKey {
	return offeringKey{provider: provider.String(), name: name}
}

// Transport carries asks to one provider.
type Transport interface {
	// Send delivers one ask. It runs on the source's sender goroutine, outside
	// the coordinator lock. An error closes the source.
	Send(*services.Ask) error
	// Close ends the transport. It is called once, after the source closes.
	Close()
}

// Register claims every (provider, name) pair for a new source, or none.
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

// markFollowersDirty gives a new source refresh work for every follow stream
// requesting one of its names.
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

// Discover enrolls a stream and freezes its initial attempt in one critical
// section. The caller has authorized every name beforehand.
func (c *Coordinator) Discover(caller *astral.Identity, names []string, follow bool) *Stream {
	return c.DiscoverSwarm(caller, names, follow, nil)
}

// DiscoverSwarm is Discover with swarm members carrying the discovery too.
// Each member owes one initial obligation per name, settled through its
// Remote; the caller has authorized every name on every member beforehand.
func (c *Coordinator) DiscoverSwarm(caller *astral.Identity, names []string, follow bool, members []*astral.Identity) *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()

	st := newStream(c, caller, names, follow)
	ck := caller.String()
	if c.byCaller[ck] == nil {
		c.byCaller[ck] = map[*Stream]struct{}{}
	}
	c.byCaller[ck][st] = struct{}{}

	at := newAttempt(st)
	st.attempt = at
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
	for _, member := range members {
		r := newContribution(st, member)
		for _, name := range names {
			o := &evaluation{at: at, contribution: r, service: name}
			at.unresolved[o] = struct{}{}
			r.unresolved[name] = o
		}
		st.contributions = append(st.contributions, r)
	}
	at.budget = c.cfg.Clock.AfterFunc(c.cfg.InitialBudget, func() { c.budgetExpired(at) })
	for _, sl := range touched {
		sl.binding.schedule(sl)
	}
	if len(at.unresolved) == 0 {
		at.finish()
	}
	return st
}

// followsService reports whether caller has an open follow stream requesting
// name.
func (c *Coordinator) followsService(ck, name string) bool {
	for st := range c.byCaller[ck] {
		if _, ok := st.names[name]; ok && st.follow && !st.closed {
			return true
		}
	}
	return false
}
