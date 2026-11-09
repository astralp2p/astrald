// Command driver runs one player app on node1 and one on node2, each on its
// own client and token. Each player serves ping and pings the other by its app
// identity: a ping-pong in both directions across the link.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

// player is one app: its identity and the client of its own token.
type player struct {
	id  *astral.Identity
	cli *astrald.Client
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_ping_pong": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]bool, error) {
	facts := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	// one player on each node, each serving ping on its own client
	p1, stop1, err := startPlayer(ctx, admin1, n1.Endpoint)
	facts["player1_serves_on_node1"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("player1 on node1: %w", err)
	}
	defer stop1()
	p2, stop2, err := startPlayer(ctx, admin2, n2.Endpoint)
	facts["player2_serves_on_node2"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("player2 on node2: %w", err)
	}
	defer stop2()

	// note: each node routes to the other's player through the player's relay
	// contract, which apphost.register pushes to the siblings in the background.
	facts["node1_knows_player2"] = waitHost(ctx, admin1, p2.id, n2.Identity)
	facts["node2_knows_player1"] = waitHost(ctx, admin2, p1.id, n1.Identity)
	if !facts["node1_knows_player2"] || !facts["node2_knows_player1"] {
		return facts, fmt.Errorf("apphost.hosts never named the other node: node1 knows player2 %v, node2 knows player1 %v",
			facts["node1_knows_player2"], facts["node2_knows_player1"])
	}

	facts["player1_pinged_player2"], err = rally(ctx, p1, p2)
	if err != nil {
		return facts, fmt.Errorf("player1 on node1 pinging player2 on node2: %w", err)
	}
	facts["player2_pinged_player1"], err = rally(ctx, p2, p1)
	if err != nil {
		return facts, fmt.Errorf("player2 on node2 pinging player1 on node1: %w", err)
	}

	fmt.Fprintln(os.Stderr, "ok: player1 on node1 and player2 on node2 pinged each other and each saw the other as caller")
	return facts, nil
}

// startPlayer registers a player app on the admin's node and serves ping on a
// client of the player's own token.
func startPlayer(ctx *astral.Context, admin *astrald.Client, endpoint string) (player, func(), error) {
	token, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return player{}, nil, fmt.Errorf("registering: %w", err)
	}
	cli := connect(endpoint, string(token.Token))
	stop, err := serve(ctx, cli)
	if err != nil {
		return player{}, nil, fmt.Errorf("serving: %w", err)
	}
	return player{id: token.Identity, cli: cli}, stop, nil
}

// rally pings to as from, once, and reports whether to answered pong to from.
func rally(ctx *astral.Context, from, to player) (bool, error) {
	reply, err := ping(ctx, from.cli, to.id)
	want := "pong, " + from.id.String()
	if err != nil {
		return false, err
	}
	if reply != want {
		return false, fmt.Errorf("got %q; want %q", reply, want)
	}
	return true, nil
}

// Player is a player app's API.
type Player struct{}

// Ping answers pong to the caller the node delivered.
func (*Player) Ping(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8("pong, " + q.Caller().String()))
}

// serve runs the player API as the app cli authenticates, through its own
// registrar. It returns once the node has attached the app's handler, or with
// the error that stopped the registration.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, cli *astrald.Client) (func(), error) {
	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, cli)))
	attached := make(chan struct{})
	var once atomic.Bool
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		if once.CompareAndSwap(false, true) {
			close(attached)
		}
		return nil
	})
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Player{}), reg, onAttach) }()

	select {
	case <-attached:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(30 * time.Second):
		cancel()
		return nil, errors.New("the app's handler was not attached within 30s")
	}
}

// waitHost polls apphost.hosts on the admin's node until it names host for
// app, within 30 seconds.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		found := false
		for h := range hosts {
			found = found || h.String() == host
		}
		if *errp == nil && found {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// ping queries app:ping as the client's app and returns the reply.
func ping(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := caller.WithTarget(app).QueryChannel(cctx, "ping", nil)
	if err != nil {
		return "", err
	}
	defer ch.Close()
	var reply *astral.String8
	if err := ch.Switch(channel.Expect(&reply), channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF()); err != nil {
		return "", err
	}
	return reply.String(), nil
}
