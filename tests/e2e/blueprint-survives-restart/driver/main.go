// Command driver is the app side of blueprint-survives-restart.
//
//	driver serve    a long-lived app with types of its own and blueprint sync on
//
// serve reads the node from APP_NODE_ENDPOINT and APP_NODE_TOKEN (an admin
// token, to register itself) and writes what it sees to the JSON file named by
// ASTRAL_TESTS_APP_STATE (tests/lib/apps.py).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: driver serve")
		os.Exit(2)
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// state is what the app reports to the harness.
type state struct {
	mu          sync.Mutex
	path        string
	Identity    string   `json:"identity"`
	Pid         int      `json:"pid"`
	Syncs       int      `json:"syncs"`       // registrations completed with the blueprint sync before them
	Disconnects []string `json:"disconnects"` // errors the registrar reported, in order
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

func serve() error {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if path == "" {
		return errors.New("ASTRAL_TESTS_APP_STATE is not set")
	}
	if err := registerTypes(); err != nil {
		return err
	}
	endpoint := os.Getenv("APP_NODE_ENDPOINT")
	admin := astrald.New(apphost.NewRouter(endpoint, os.Getenv("APP_NODE_TOKEN")))
	ctx := astral.NewContext(nil)
	token, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return fmt.Errorf("registering: %w", err)
	}
	cli := astrald.New(apphost.NewRouter(endpoint, string(token.Token)))
	// why: apps.SyncBlueprints talks to the process-wide default client.
	astrald.SetDefault(cli)

	st := &state{path: path, Identity: token.Identity.String(), Pid: os.Getpid(), Disconnects: []string{}}

	sctx, cancel := ctx.WithCancel()
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; cancel() }()

	reg := apps.NewAppRegistrar(sctx,
		apps.WithClient(apphostcli.New(nil, cli)),
		apps.WithEvents(apps.AppRegistrarEvents{OnDisconnect: func(err error) {
			st.update(func() { st.Disconnects = append(st.Disconnects, fmt.Sprint(err)) })
		}}))
	// why a hook after the sync: hooks run in the order given, on the first
	// attach and on every reconnect, so Syncs counts completed syncs.
	counted := apps.WithRegistrationHook(func(*astral.Context) error {
		st.update(func() { st.Syncs++ })
		return nil
	})
	st.update(func() {})
	err = apps.ServeWith(sctx, routing.NewOpRouter(), reg, apps.WithBlueprintSync(), counted)
	if sctx.Err() != nil {
		return nil
	}
	return err
}
