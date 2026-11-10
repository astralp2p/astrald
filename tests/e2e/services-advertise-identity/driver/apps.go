package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// node is one node of the session view.
type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

func loadNodes(path string) (map[string]node, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return s.Nodes, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// app is a registered app identity with a client of its own.
type app struct {
	id     *astral.Identity
	client *astrald.Client
	n      node
}

// register mints a fresh app identity on n through the node's admin token. The
// node grants it ServeApps, as it does every app it registers.
func register(ctx *astral.Context, n node) (*app, error) {
	tok, err := apphostcli.New(nil, connect(n.Endpoint, n.Token)).Register(ctx)
	if err != nil {
		return nil, err
	}
	return &app{id: tok.Identity, client: connect(n.Endpoint, string(tok.Token)), n: n}, nil
}

func revokeServeApps(ctx *astral.Context, a *app) error {
	return apphostcli.New(nil, connect(a.n.Endpoint, a.n.Token)).RevokeAction(ctx, a.id, auth.ServeAppsAction{}.ObjectType())
}

// grantDiscovery lets a discover the named services on any node, granted by
// the admin of a's node.
func grantDiscovery(ctx *astral.Context, a *app, names ...string) error {
	rule := &services.DiscoveryRule{}
	for _, n := range names {
		rule.Services = append(rule.Services, astral.String8(n))
	}
	cs := astral.NewBundle()
	if err := cs.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return err
	}
	return apphostcli.New(nil, connect(a.n.Endpoint, a.n.Token)).Grant(ctx, &apphostapi.GrantRequest{
		Identity: a.id,
		Permits:  []*auth.Permit{{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}},
	})
}

// closer runs the closers it was given, once each, in reverse order.
type closer struct {
	mu  sync.Mutex
	fns []func()
}

func (c *closer) add(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fns = append(c.fns, fn)
}

func (c *closer) closeAll() {
	c.mu.Lock()
	fns := c.fns
	c.fns = nil
	c.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// binding is an open advertisement made through the SDK. Every service it
// offers is available and carries info; it remembers who it evaluated.
type binding struct {
	b     *servicescli.Binding
	close func()

	mu   sync.Mutex
	seen []string
}

func advertise(ctx *astral.Context, a *app, names []string) (*binding, error) {
	return advertiseOn(ctx, a, nil, names)
}

// advertiseOn advertises as a on target (a's own node when nil).
func advertiseOn(ctx *astral.Context, a *app, target *astral.Identity, names []string) (*binding, error) {
	bd := &binding{}
	b, err := servicescli.New(target, a.client).Advertise(ctx, names, func(_ *astral.Context, caller *astral.Identity, _ string) (*services.Update, error) {
		bd.mu.Lock()
		bd.seen = append(bd.seen, caller.String())
		bd.mu.Unlock()
		bundle := astral.NewBundle()
		if err := bundle.Append(astral.NewString8(info)); err != nil {
			return nil, err
		}
		return &services.Update{Available: true, Info: bundle}, nil
	})
	if err != nil {
		return nil, err
	}
	bd.b = b
	bd.close = sync.OnceFunc(func() { _ = b.Close() })
	return bd, nil
}

func (bd *binding) saw(id *astral.Identity) bool {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	for _, s := range bd.seen {
		if s == id.String() {
			return true
		}
	}
	return false
}

func (bd *binding) callers() []string {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	return append([]string(nil), bd.seen...)
}

// answer is what a hand-written binding sends for one ask: the SDK always
// leaves provider empty and names the service asked, so only a hand-written
// binding can say otherwise.
type answer struct {
	name     string
	provider *astral.Identity
}

// rawBinding is an advertisement written against the wire: the app reads each
// ask and decides the answer itself.
type rawBinding struct {
	app   *app
	ch    *channel.Channel
	done  chan struct{}
	close func()
}

// rawAdvertise registers a fresh app on n and advertises name as it, answering
// every ask with reply(app id, service asked). The binding closes with cl.
func rawAdvertise(ctx *astral.Context, n node, cl *closer, name string, reply func(me *astral.Identity, service string) answer) (*rawBinding, error) {
	a, err := register(ctx, n)
	if err != nil {
		return nil, err
	}
	ch, err := a.client.QueryChannel(ctx, services.MethodAdvertise, query.Args{"services": name}, channel.WithLockedWrites())
	if err != nil {
		return nil, fmt.Errorf("advertising %v by hand: %w", name, err)
	}
	if err := ch.Switch(channel.ExpectAck, channel.PassErrors); err != nil {
		ch.Close()
		return nil, fmt.Errorf("advertising %v by hand: %w", name, err)
	}
	rb := &rawBinding{app: a, ch: ch, done: make(chan struct{})}
	rb.close = sync.OnceFunc(func() { _ = ch.Close() })
	cl.add(rb.close)
	go func() {
		defer close(rb.done)
		for {
			o, err := ch.Receive()
			if err != nil {
				return
			}
			ask, ok := o.(*services.Ask)
			if !ok {
				continue
			}
			r := reply(a.id, string(ask.Service))
			_ = ch.Send(&services.Answer{
				RequestID: ask.RequestID,
				Update:    &services.Update{Available: true, Name: astral.String8(r.name), ProviderID: r.provider},
			})
		}
	}()
	return rb, nil
}

// ended reports whether the node ended the binding, within a few seconds.
func (rb *rawBinding) ended() bool {
	select {
	case <-rb.done:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

// offering is one discovered service and who provides it.
type offering struct {
	name     string
	provider *astral.Identity
}

func (o offering) String() string { return fmt.Sprintf("%v/%v", o.provider, o.name) }

// discovery is a one-shot discovery read to its end.
type discovery struct {
	updates []*services.Update
	outcome *servicescli.InitialOutcome
	err     error
}

func discoverOnce(ctx *astral.Context, a *app, reach services.Reach, names ...string) discovery {
	cctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	events, err := servicescli.New(nil, a.client).DiscoverIn(cctx, reach, names, false)
	if err != nil {
		return discovery{err: err}
	}
	var d discovery
	for ev := range events {
		switch {
		case ev.Update != nil && bool(ev.Update.Available):
			d.updates = append(d.updates, ev.Update)
		case ev.Initial != nil:
			d.outcome = ev.Initial
		case ev.Err != nil:
			d.err = ev.Err
		}
	}
	return d
}

func swarm(ctx *astral.Context, a *app, names ...string) discovery {
	return discoverOnce(ctx, a, services.ReachSwarm, names...)
}

func local(ctx *astral.Context, a *app, names ...string) discovery {
	return discoverOnce(ctx, a, services.ReachLocal, names...)
}

func (d discovery) complete() bool { return d.err == nil && d.outcome != nil && d.outcome.Complete }
func (d discovery) hasErr() bool   { return d.err != nil }
func (d discovery) none() bool     { return len(d.updates) == 0 }

func (d discovery) offerings() []string {
	var out []string
	for _, u := range d.updates {
		out = append(out, offering{string(u.Name), u.ProviderID}.String())
	}
	sort.Strings(out)
	return out
}

// exactly reports whether the discovery holds the wanted offerings, each once.
func (d discovery) exactly(want ...offering) bool {
	var w []string
	for _, o := range want {
		w = append(w, o.String())
	}
	sort.Strings(w)
	return strings.Join(d.offerings(), ",") == strings.Join(w, ",")
}

// carries reports whether every offering carries text in its info.
func (d discovery) carries(text string) bool {
	for _, u := range d.updates {
		found := false
		if u.Info != nil {
			for _, o := range u.Info.Objects() {
				if s, ok := o.(*astral.String8); ok && s.String() == text {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return len(d.updates) > 0
}

func (d discovery) String() string {
	return fmt.Sprintf("offerings %v, outcome %+v, err %v", d.offerings(), d.outcome, d.err)
}

// waitNone waits until a swarm discovery of names from a shows no provider.
func waitNone(ctx *astral.Context, a *app, names ...string) bool {
	deadline := time.Now().Add(20 * time.Second)
	for {
		d := swarm(ctx, a, names...)
		if d.none() && d.complete() {
			return true
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "still shown: %v\n", d)
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

var (
	notPermitted = auth.ErrNotPermitted.Error()
	advertised   = services.ErrAdvertised.Error()
)

func isNotPermitted(err error) bool { return err != nil && err.Error() == notPermitted }
func isAdvertised(err error) bool {
	return err != nil && (errors.Is(err, services.ErrAdvertised) || err.Error() == advertised)
}

func step(facts map[string]bool, name string, ok bool, format string, args ...any) error {
	facts[name] = ok
	if !ok {
		return fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	}
	fmt.Fprintln(os.Stderr, "ok:", name)
	return nil
}
