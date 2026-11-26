// Command follower is the long-lived app of expel-offline-subject: an app on
// node3 that holds a swarm follow of one service across node3's restart.
//
//	follower <session.json>
//
// It registers an app on node3, grants it discovery of the service on node3,
// and follows the service with reach swarm, reopening the follow whenever the
// stream ends (WithReconnect). It writes its state to the JSON file named by
// ASTRAL_TESTS_APP_STATE: the app identity and how many follows reached their
// boundary, with the time of the last one. It runs until signalled.
//
// why Go: astral-py speaks the pre-v1 Services wire (services-swarm-membership).
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

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// service is the name the follow asks for. No node offers it: a swarm follow
// carries to every member whatever the members offer.
const service = "expel-offline-subject"

// state is what the follower reports to the harness.
type state struct {
	mu           sync.Mutex
	path         string
	App          string  `json:"app"`
	Boundaries   int     `json:"boundaries"`    // follows that reached their boundary
	LastBoundary float64 `json:"last_boundary"` // unix seconds of the last boundary
}

func (s *state) update(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
	raw, _ := json.Marshal(s)
	// why rename: the reader polls the file and must never see half of it
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: follower <session.json>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	statePath := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if statePath == "" {
		return errors.New("ASTRAL_TESTS_APP_STATE is not set")
	}
	s, err := provider.Load(path)
	if err != nil {
		return err
	}
	n3, err := s.Node("node3")
	if err != nil {
		return err
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; cancel() }()

	watch, app, err := follow(ctx, n3)
	if err != nil {
		return err
	}
	defer watch.Close()

	st := &state{path: statePath, App: app.String()}
	st.update(func() {})
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-watch.Done():
			return fmt.Errorf("the follow ended for good: %v", watch.Err())
		case <-watch.Synced():
			st.update(func() {
				st.Boundaries++
				st.LastBoundary = float64(time.Now().UnixNano()) / 1e9
			})
		}
	}
}

// follow registers an app on n, grants it discovery of service on n, and
// follows service with reach swarm, reopening after every end.
func follow(ctx *astral.Context, n provider.Node) (*servicescli.Watcher, *astral.Identity, error) {
	admin := n.Admin()
	app, err := provider.Register(ctx, admin, n)
	if err != nil {
		return nil, nil, err
	}
	scope := astral.NewBundle()
	rule := &services.DiscoveryRule{Services: []astral.String8{service}}
	if err := scope.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return nil, nil, err
	}
	err = provider.Grant(ctx, app, &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: scope})
	if err != nil {
		return nil, nil, fmt.Errorf("grant discovery on %v: %w", n.Identity, err)
	}
	// why a short backoff: the follow reopens within 2 s of node3's restart,
	// so a carry restarted on a node that still believes it is a member shows.
	watch, err := servicescli.New(nil, app.Client).WatchIn(ctx, services.ReachSwarm, []string{service},
		servicescli.WithReconnect(time.Second, 2*time.Second))
	if err != nil {
		return nil, nil, fmt.Errorf("follow on %v: %w", n.Identity, err)
	}
	return watch, app.ID, nil
}
