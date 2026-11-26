// Command driver runs the relay-contract-forwarding story and prints its facts
// as one JSON line.
//
// A greeter app registers on node2 and node1 learns it. Dialing is switched
// off on node2 and node3, so no link between them can form. The User adopts
// node3 on node1, and node3 must name node2 as the greeter's host from the
// relay contract node1 forwards, with no link to node2. Then node3 may dial
// again, and a visitor app on node3 calls the greeter through node2.
//
//	driver <session.json>
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/joins"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/nodes"
	nodescli "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// converge bounds every wait for an asynchronous push or sync.
const converge = 30 * time.Second

// dialSettings are the tree settings that let a node dial out, one per transport.
var dialSettings = []string{"/mod/tcp/settings/dial", "/mod/kcp/settings/dial", "/mod/tor/settings/dial"}

type node struct {
	Endpoint    string `json:"endpoint"`
	Token       string `json:"token"`
	Identity    string `json:"identity"`
	HostIP      string `json:"host_ip"`
	LanEndpoint string `json:"lan_endpoint"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
	} `json:"facts"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	facts := map[string]any{}
	err := run(os.Args[1], facts)
	out, _ := json.Marshal(map[string]any{"relay_contract_forwarding": facts})
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string, facts map[string]any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	n1, n2, n3 := s.Nodes["node1"], s.Nodes["node2"], s.Nodes["node3"]
	ctx := astral.NewContext(nil)
	admin1, admin2, admin3 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token), connect(n3.Endpoint, n3.Token)

	// the greeter app on node2, known to node1 through apphost.register's push
	greeter, err := contracts.Register(ctx, admin2, n2.Endpoint)
	if err != nil {
		return fmt.Errorf("registering the greeter on node2: %w", err)
	}
	facts["greeter"] = greeter.Identity.String()
	stop, err := serve(ctx, n2, string(greeter.Token))
	if err != nil {
		return fmt.Errorf("the greeter could not serve on node2: %w", err)
	}
	defer stop()
	if !waitHost(ctx, admin1, greeter.Identity, n2.Identity) {
		return fmt.Errorf("apphost.hosts on node1 never named node2 for the greeter %v", greeter.Identity)
	}

	// why both: node2 would dial node3 once node1 pushed node3's contract, and
	// node3 would dial node2 once node1 synced node2's contract to it.
	for _, admin := range []*astrald.Client{admin2, admin3} {
		if err := setDial(ctx, admin, false); err != nil {
			return err
		}
	}
	facts["dialing_off_on_node2_and_node3"] = true

	if err := adopt(ctx, s, n1, n3); err != nil {
		return err
	}
	facts["node3_adopted_on_node1"] = true

	start := time.Now()
	facts["node3_knows_the_greeter_on_node2"] = waitHost(ctx, admin3, greeter.Identity, n2.Identity)
	facts["seconds_to_learn"] = time.Since(start).Seconds()
	if !facts["node3_knows_the_greeter_on_node2"].(bool) {
		return fmt.Errorf("apphost.hosts on node3 never named node2 for the greeter within %v", converge)
	}

	linked, err := isLinked(ctx, admin3, n2.Identity)
	if err != nil {
		return err
	}
	facts["node3_unlinked_from_node2_when_it_knew"] = !linked
	if linked {
		return errors.New("node3 holds a link to node2, so node2 may have delivered the contract itself")
	}

	return callThroughNode2(ctx, n3, admin3, greeter.Identity, facts)
}

// callThroughNode2 lets node3 dial again and calls the greeter from a visitor
// app on node3, which node3 routes through node2.
func callThroughNode2(ctx *astral.Context, n3 node, admin3 *astrald.Client, greeter *astral.Identity, facts map[string]any) error {
	if err := setDial(ctx, admin3, true); err != nil {
		return err
	}

	visitor, err := contracts.Register(ctx, admin3, n3.Endpoint)
	if err != nil {
		return fmt.Errorf("registering the visitor on node3: %w", err)
	}
	reply, err := hello(ctx, connect(n3.Endpoint, string(visitor.Token)), greeter)
	want := "hello, " + visitor.Identity.String()
	facts["visitor_on_node3_greeted_through_node2"] = err == nil && reply == want
	if err != nil || reply != want {
		return fmt.Errorf("the visitor on node3 called greeter:hello and got %q (err %v); want %q", reply, err, want)
	}
	return nil
}

// adopt makes node1 and node3 know each other's endpoint, links node1 to
// node3, and admits node3 through node1 as a Member.
//
// why node1 dials: a joiner reaches its carrier (user.join), and node3's
// dialing is off, so node3's join runs over the link node1 opened; before G7
// user.adopt ran on node1 and node1 dialed itself.
func adopt(ctx *astral.Context, s session, n1, n3 node) error {
	admin1 := connect(n1.Endpoint, n1.Token)
	if err := addEndpoint(ctx, admin1, n3); err != nil {
		return err
	}
	if err := addEndpoint(ctx, connect(n3.Endpoint, n3.Token), n1); err != nil {
		return err
	}
	if err := newLink(ctx, admin1, n3); err != nil {
		return err
	}
	// note: user.adopt is gone (G7); node3 joins through node1 as a Member
	if _, err := joins.Admit(ctx, connect(n3.Endpoint, n3.Token), connect(n1.Endpoint, s.Facts.UserToken), n1.Identity, n3.Identity); err != nil {
		return fmt.Errorf("adopting node3 on node1: %w", err)
	}
	return nil
}

// newLink asks the admin's node to link to peer at its lan endpoint.
func newLink(ctx *astral.Context, admin *astrald.Client, peer node) error {
	lctx, cancel := ctx.WithTimeout(converge)
	defer cancel()
	ch, err := admin.QueryChannel(lctx, "nodes.new_link", query.Args{"identity": peer.Identity, "endpoint": peer.LanEndpoint})
	if err != nil {
		return fmt.Errorf("nodes.new_link %v: %w", peer.Identity, err)
	}
	defer ch.Close()
	var info *nodes.LinkInfo
	if err := ch.Switch(channel.Expect(&info), channel.PassErrors, channel.WithContext(lctx), channel.FailOnEOF()); err != nil {
		return fmt.Errorf("nodes.new_link %v: %w", peer.Identity, err)
	}
	return nil
}

func addEndpoint(ctx *astral.Context, admin *astrald.Client, peer node) error {
	ch, err := admin.QueryChannel(ctx, "nodes.add_endpoint", query.Args{"identity": peer.Identity, "endpoint": peer.LanEndpoint})
	if err != nil {
		return fmt.Errorf("nodes.add_endpoint %v: %w", peer.Identity, err)
	}
	defer ch.Close()
	if err := ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return fmt.Errorf("nodes.add_endpoint %v: %w", peer.Identity, err)
	}
	return nil
}

// setDial switches dialing on every transport of the admin's node.
//
// note: a raw tree.set, not tree.Query: the astral-go tree client's Sub
// shares one backing array between sibling paths, so a walk to
// settings/dial may come back as settings/listen.
func setDial(ctx *astral.Context, admin *astrald.Client, on bool) error {
	for _, path := range dialSettings {
		if err := setValue(ctx, admin, path, astral.Bool(on)); err != nil {
			return fmt.Errorf("tree.set %v=%v: %w", path, on, err)
		}
	}
	return nil
}

func setValue(ctx *astral.Context, admin *astrald.Client, path string, v astral.Bool) error {
	ch, err := admin.QueryChannel(ctx, tree.MethodSet, query.Args{"path": path})
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.Send(&v); err != nil {
		return err
	}
	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF())
}

func isLinked(ctx *astral.Context, admin *astrald.Client, peer string) (bool, error) {
	links, err := nodescli.New(nil, admin).Links(ctx)
	if err != nil {
		return false, fmt.Errorf("nodes.links: %w", err)
	}
	for _, l := range links {
		if l.RemoteIdentity.String() == peer {
			return true, nil
		}
	}
	return false, nil
}

// Greeter is the greeter app's API.
type Greeter struct{}

// Hello greets the caller by the identity the node delivered.
func (*Greeter) Hello(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8("hello, " + q.Caller().String()))
}

// serve runs the greeter API as the app that token authenticates, and returns
// once the node has attached the app's handler.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, n node, token string) (func(), error) {
	cli := apphostcli.New(nil, connect(n.Endpoint, token))
	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(cli))
	attached := make(chan struct{})
	var once atomic.Bool
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		if once.CompareAndSwap(false, true) {
			close(attached)
		}
		return nil
	})
	// why host_ip: the node dials the handler's registered endpoint, and only
	// an address at the node's host_ip reaches the test host
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(n.HostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Greeter{}), reg, at, onAttach) }()

	select {
	case <-attached:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(converge):
		cancel()
		return nil, errors.New("the app's handler was not attached in time")
	}
}

// waitHost polls apphost.hosts on the admin's node until it names host for app.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(converge)
	for time.Now().Before(deadline) {
		hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		found := false
		for h := range hosts {
			found = found || h.String() == host
		}
		if *errp == nil && found {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// hello queries app:hello as the client's app and returns the reply.
func hello(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(converge)
	defer cancel()
	ch, err := caller.WithTarget(app).QueryChannel(cctx, "hello", nil)
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
