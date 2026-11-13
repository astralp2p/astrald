package provider

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/routing"
)

// Op names one objects op a Provider serves.
type Op string

const (
	Find     Op = "find"
	Describe Op = "describe"
	Search   Op = "search"
	Read     Op = "read"
)

// Mode is how a Provider's handler answers one op.
type Mode int

const (
	// Answer replies with the configured answer: no identity for find, the
	// descriptors for describe, the results for search, the bytes for read
	// (or a reject for an object it has no bytes for).
	Answer Mode = iota
	// Claim is find naming every identity of the claim list, repeats
	// included.
	Claim
	// Hang accepts the query and holds it open, unanswered, until the caller
	// goes away or the app stops.
	Hang
)

// hangLimit bounds a Hang handler that no caller ever releases.
const hangLimit = 2 * time.Minute

// Provider is an app that serves objects.find, objects.describe,
// objects.search and objects.read, and counts every query of each op by key:
// the object id for find, describe and read, the query text for search.
type Provider struct {
	App *App

	mu          sync.Mutex
	modes       map[Op]Mode
	claims      []*astral.Identity
	descriptors []*objects.Descriptor
	results     []*objects.SearchResult
	data        map[string][]byte
	counts      map[Op]map[string]int
	holds       map[Op]map[string][]time.Duration
	stopped     chan struct{}
}

// New returns a Provider for app, every op in Answer mode with an empty
// answer.
func New(app *App) *Provider {
	return &Provider{
		App:     app,
		modes:   map[Op]Mode{},
		data:    map[string][]byte{},
		counts:  map[Op]map[string]int{},
		holds:   map[Op]map[string][]time.Duration{},
		stopped: make(chan struct{}),
	}
}

// SetMode sets how the provider answers op.
func (p *Provider) SetMode(op Op, m Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modes[op] = m
}

// SetClaims replaces the claim list find names in Claim mode.
func (p *Provider) SetClaims(ids ...*astral.Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = append([]*astral.Identity(nil), ids...)
}

// SetDescriptors replaces what describe answers for an id: each descriptor
// whose ObjectID is that id, sent as set, SourceID included.
func (p *Provider) SetDescriptors(ds ...*objects.Descriptor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.descriptors = append([]*objects.Descriptor(nil), ds...)
}

// SetResults replaces what search answers for any query, sent as set,
// SourceID included.
func (p *Provider) SetResults(rs ...*objects.SearchResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = append([]*objects.SearchResult(nil), rs...)
}

// SetData sets the bytes read answers for id.
func (p *Provider) SetData(id *astral.ObjectID, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data[id.String()] = data
}

// Count returns how many op queries for key reached the provider.
func (p *Provider) Count(op Op, key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[op][key]
}

// Total returns how many op queries reached the provider, any key.
func (p *Provider) Total(op Op) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.counts[op] {
		n += c
	}
	return n
}

// Holds returns how long each Hang of an op query for key was held before its
// caller closed it, in the order they were released. A hold the caller never
// closed is not listed.
func (p *Provider) Holds(op Op, key string) []time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Duration(nil), p.holds[op][key]...)
}

// hit counts one op query for key and returns the op's mode.
func (p *Provider) hit(op Op, key string) Mode {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.counts[op] == nil {
		p.counts[op] = map[string]int{}
	}
	p.counts[op][key]++
	return p.modes[op]
}

// Serve serves the provider on its app's node through apps.ServeWith with the
// app's own client, on a handler bound at the app's HostIP. hooks run after
// each registration of the handler, in order. Serve returns once the first registration and its hooks succeeded;
// the returned stop ends the serving.
func (p *Provider) Serve(ctx *astral.Context, hooks ...apps.RegistrationHook) (stop func(), err error) {
	// why the host IP: the node dials the handler's registered endpoint for
	// every query, and only an address at the node's host_ip reaches the test
	// host from the node's machine
	if p.App.HostIP == "" {
		return nil, fmt.Errorf("serving app %v: its node has no host_ip", p.App.ID)
	}
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(p.App.HostIP, "0"))
	app := routing.NewApp(&struct{}{})
	app.Add("objects", &ops{p: p})

	ready := make(chan struct{})
	var once sync.Once
	hooks = append(hooks, func(*astral.Context) error {
		once.Do(func() { close(ready) })
		return nil
	})

	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, p.App.Client)))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, app, reg, at, apps.WithRegistrationHooks(hooks...)) }()

	stop = sync.OnceFunc(func() {
		cancel()
		close(p.stopped)
	})
	select {
	case <-ready:
		return stop, nil
	case err := <-served:
		stop()
		return nil, fmt.Errorf("serving app %v: %v", p.App.ID, err)
	case <-time.After(60 * time.Second):
		stop()
		return nil, fmt.Errorf("serving app %v: not registered within 60 s", p.App.ID)
	}
}

// hang holds an accepted op query for key open until its caller closes it,
// the provider stops, or hangLimit passes. A close by the caller is recorded
// in Holds.
func (p *Provider) hang(op Op, key string, conn io.ReadWriteCloser) {
	defer conn.Close()
	start := time.Now()
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		_, _ = io.Copy(io.Discard, conn)
	}()
	select {
	case <-gone:
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.holds[op] == nil {
			p.holds[op] = map[string][]time.Duration{}
		}
		p.holds[op][key] = append(p.holds[op][key], time.Since(start))
	case <-p.stopped:
	case <-time.After(hangLimit):
	}
}
