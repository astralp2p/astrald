// Command driver is the app side of services-consumer-restart.
//
//	driver provide   a long-lived provider app (started by tests/lib/apps.py)
//	driver watch     a long-lived consumer app that follows the service with a
//	                 reconnecting Watcher (astral-go servicescli.WithReconnect)
//
// Each reads the node it acts on from APP_NODE_ENDPOINT and APP_NODE_TOKEN (the
// node's admin token: the apps register themselves and the consumer requests
// its own discovery permit) and writes what it sees to the JSON file named by
// ASTRAL_TESTS_APP_STATE.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// service is the one name this test advertises and follows.
const service = "services-consumer-restart-echo"

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: driver provide | watch")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "provide":
		err = provide()
	case "watch":
		err = watch()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(token string) *astrald.Client {
	return astrald.New(apphost.NewRouter(os.Getenv("APP_NODE_ENDPOINT"), token))
}

// register mints an app identity on the node, with permits, and returns it with
// a client of its own token.
func register(ctx *astral.Context, permits ...*auth.Permit) (*astral.Identity, *astrald.Client, error) {
	token, err := contracts.Register(ctx, connect(os.Getenv("APP_NODE_TOKEN")), os.Getenv("APP_NODE_ENDPOINT"), permits...)
	if err != nil {
		return nil, nil, fmt.Errorf("registering: %w", err)
	}
	return token.Identity, connect(string(token.Token)), nil
}

// requestDiscovery has app request a discovery permit for service, approved by
// the harness answerer, so what the follow sees does not depend on the
// swarm-app rule.
func requestDiscovery(ctx *astral.Context, app *astrald.Client) error {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{service}}}}
	if err := cs.Append(scope); err != nil {
		return err
	}
	_, err := contracts.Request(ctx, app, &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs})
	return err
}

// stateFile rewrites its whole JSON file, by rename, after every update, on a
// goroutine of its own: the harness polls it and must never see half of it.
type stateFile struct {
	mu    sync.Mutex
	path  string
	v     any
	dirty chan struct{}
}

func newStateFile(v any) (*stateFile, error) {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if path == "" {
		return nil, errors.New("ASTRAL_TESTS_APP_STATE is not set")
	}
	s := &stateFile{path: path, v: v, dirty: make(chan struct{}, 1)}
	go s.writer()
	return s, nil
}

// update applies f under the lock and schedules a write of the result.
func (s *stateFile) update(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *stateFile) writer() {
	for range s.dirty {
		s.mu.Lock()
		raw, _ := json.Marshal(s.v)
		s.mu.Unlock()
		tmp := s.path + ".tmp"
		if os.WriteFile(tmp, raw, 0o600) == nil {
			os.Rename(tmp, s.path)
		}
	}
}

func untilSignal(ctx *astral.Context) (*astral.Context, func()) {
	sctx, cancel := ctx.WithCancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; cancel() }()
	return sctx, cancel
}

// providerState is what a provider reports.
type providerState struct {
	Identity   string `json:"identity"`
	Pid        int    `json:"pid"`
	Advertised bool   `json:"advertised"` // the node acknowledged the binding
}

// provide registers a provider app, which holds ServeApps, and keeps one open
// advertise binding of the service until signalled.
func provide() error {
	ctx := astral.NewContext(nil)
	id, cli, err := register(ctx, serveApps)
	if err != nil {
		return err
	}
	ps := &providerState{Identity: id.String(), Pid: os.Getpid()}
	st, err := newStateFile(ps)
	if err != nil {
		return err
	}
	sctx, cancel := untilSignal(ctx)
	defer cancel()
	st.update(func() {})

	p := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		service: func(*astral.Context, *astral.Identity) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		},
	})
	defer p.Close()
	if err := p.Serve(sctx, servicescli.New(nil, cli)); err != nil {
		return fmt.Errorf("advertising: %w", err)
	}
	st.update(func() { ps.Advertised = true })
	<-sctx.Done()
	return nil
}

// watcherState is what the consumer's Watcher shows, rewritten on every
// change of the Watcher.
type watcherState struct {
	Identity  string   `json:"identity"`
	Pid       int      `json:"pid"`
	Offerings []string `json:"offerings"` // provider identities, sorted
	Stale     bool     `json:"stale"`
	Synced    int      `json:"synced"`  // Synced signals seen (coalesced)
	Ends      int      `json:"ends"`    // times Stale turned true: a follow ended
	Initial   string   `json:"initial"` // the first boundary's outcome, once it came
	Err       string   `json:"err"`     // the Watcher stopped, and why
	At        int64    `json:"at"`      // unix milliseconds of the last change
}

// watch registers a consumer app, requests its discovery permit, and follows the service
// with reach=swarm and the SDK's default reconnect backoff until signalled.
func watch() error {
	ctx := astral.NewContext(nil)
	id, cli, err := register(ctx)
	if err != nil {
		return err
	}
	if err := requestDiscovery(ctx, cli); err != nil {
		return fmt.Errorf("requesting discovery: %w", err)
	}
	ws := &watcherState{Identity: id.String(), Pid: os.Getpid(), Offerings: []string{}}
	st, err := newStateFile(ws)
	if err != nil {
		return err
	}
	sctx, cancel := untilSignal(ctx)
	defer cancel()
	st.update(func() {})

	w, err := servicescli.New(nil, cli).WatchIn(sctx, services.ReachSwarm, []string{service}, servicescli.WithReconnect(0, 0))
	if err != nil {
		return fmt.Errorf("watching: %w", err)
	}
	defer w.Close()
	record(w, st, ws)
	if sctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("the watcher stopped: %w", w.Err())
}

// record writes the Watcher's state after every signal until it stops.
func record(w *servicescli.Watcher, st *stateFile, ws *watcherState) {
	initial := w.Initial()
	for {
		synced := false
		select {
		case o, ok := <-initial:
			initial = nil
			if ok {
				st.update(func() { ws.Initial = fmt.Sprintf("complete=%v incomplete=%v", o.Complete, o.Incomplete) })
			}
		case <-w.Synced():
			synced = true
		case <-w.Changed():
		case <-w.Done():
			st.update(func() { ws.Err = fmt.Sprint(w.Err()) })
			return
		}
		st.update(func() { snapshot(w, ws, synced) })
	}
}

func snapshot(w *servicescli.Watcher, ws *watcherState, synced bool) {
	stale := w.Stale()
	if stale && !ws.Stale {
		ws.Ends++
	}
	ws.Stale = stale
	if synced {
		ws.Synced++
	}
	ws.Offerings = ws.Offerings[:0]
	for _, u := range w.Offerings() {
		ws.Offerings = append(ws.Offerings, u.ProviderID.String())
	}
	ws.At = time.Now().UnixMilli()
}
