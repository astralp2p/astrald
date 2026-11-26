// Command driver runs the reconnect-sync-after-dead-link story and prints its
// facts as one JSON line.
//
// node2 links to node1 through a TCP relay on the test host, and every other
// link between them is closed. Once the relay has been quiet for a second, it
// drops every byte while keeping both connections open, so each node holds a link that takes bytes and never
// answers. An app registers on node1, and its relay contract push to node2
// goes into the dead link. node2 then links to node1 directly. node1 must close
// the relayed link within its ping timeout and push the app's relay contract to
// node2 over the new link.
//
//	driver <session.json>
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	// why: nodes.LinkInfo carries its endpoints as typed objects (mod.tcp.endpoint,
	// mod.kcp.endpoint, ...), and only the SDK's root package registers their blueprints.
	_ "github.com/astralp2p/astral-go"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/nodes"
	nodescli "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// converge bounds every wait for a link change or a sync.
const converge = 30 * time.Second

// pushSettle outlasts the 15 s bound of a sibling push (astrald mod/user/src/sync.go,
// siblingPushTimeout), so the registration's push has failed before node2 relinks.
const pushSettle = 17 * time.Second

// relayQuiet is how long the relay forwards nothing before it black-holes.
// why: the syncs a new link starts (LinkCreated, partial close) are still in
// flight when relayOnly returns; a caller-proof push over the link that times out
// wakes its ping (mux.go onSilentTimeout), the 3 s ping timeout then closes the
// link inside pushSettle, and node2 relinks directly and learns the app.
const relayQuiet = time.Second

type node struct {
	Endpoint    string `json:"endpoint"`
	Token       string `json:"token"`
	Identity    string `json:"identity"`
	HostIP      string `json:"host_ip"`
	LanEndpoint string `json:"lan_endpoint"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	facts := map[string]any{}
	err := run(os.Args[1], facts)
	out, _ := json.Marshal(map[string]any{"reconnect_sync": facts})
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// pair is node1 and node2 with an admin client on each.
type pair struct {
	n1, n2         node
	admin1, admin2 *astrald.Client
}

func run(path string, facts map[string]any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	p := pair{n1: n1, n2: n2, admin1: connect(n1.Endpoint, n1.Token), admin2: connect(n2.Endpoint, n2.Token)}
	ctx := astral.NewContext(nil)

	relay, err := startRelay(n2.HostIP, strings.TrimPrefix(n1.LanEndpoint, "tcp:"))
	if err != nil {
		return err
	}
	defer relay.Close()

	relayed, err := relayOnly(ctx, p, relay)
	if err != nil {
		return err
	}
	facts["only_the_relayed_link_left"] = true

	if !relay.waitQuiet(relayQuiet, converge) {
		return errors.New("relayed link never went quiet")
	}
	relay.blackhole()
	app, err := registerUnreached(ctx, p)
	if app != nil {
		facts["app"] = app.String()
	}
	facts["node2_missed_the_app"] = err == nil
	if err != nil {
		return err
	}

	return relink(ctx, p, relayed, app, facts)
}

// registerUnreached registers an app on node1 while node2 is out of reach, and
// checks that node2 has not learned it once the registration's push failed.
//
// why pushSettle counts from the registration and not from Register's return:
// apphost.register pushes the relay contract at once, but Register returns only
// once the host signed the app's contract for an Approval, seconds later;
// counted from the return, the check comes after the push's 15 s bound and the
// 3 s ping it wakes have closed the relayed link, and node2 has relinked.
func registerUnreached(ctx *astral.Context, p pair) (*astral.Identity, error) {
	start := time.Now()
	app, err := contracts.Register(ctx, p.admin1, p.n1.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("registering the app on node1: %w", err)
	}
	wait := time.Until(start.Add(pushSettle))
	if wait <= 0 {
		return app.Identity, fmt.Errorf("registering the app on node1 took %v, past pushSettle", time.Since(start))
	}
	time.Sleep(wait)
	known, err := knowsHost(ctx, p.admin2, app.Identity, p.n1.Identity)
	if err != nil {
		return app.Identity, err
	}
	if known {
		return app.Identity, errors.New("node2 learned the app through the black-holed link; the relay did not drop its bytes")
	}
	return app.Identity, nil
}

// relink links node2 to node1 directly, then waits for node1 to drop the
// relayed link and for node2 to learn the app on node1.
func relink(ctx *astral.Context, p pair, relayed astral.Nonce, app *astral.Identity, facts map[string]any) error {
	start := time.Now()
	if _, err := newLink(ctx, p.admin2, p.n1.Identity, p.n1.LanEndpoint); err != nil {
		return fmt.Errorf("linking node2 to node1 directly: %w", err)
	}

	dropped := waitLinks(ctx, p.admin1, p.n2.Identity, func(ids []astral.Nonce) bool { return !contains(ids, relayed) })
	facts["seconds_to_drop"] = time.Since(start).Seconds()
	facts["node1_dropped_the_relayed_link"] = dropped
	if !dropped {
		return fmt.Errorf("node1 still held the black-holed link %v %v after the new link", relayed, converge)
	}

	learned := waitHost(ctx, p.admin2, app, p.n1.Identity)
	facts["seconds_to_learn"] = time.Since(start).Seconds()
	facts["node2_knows_the_app_on_node1"] = learned
	if !learned {
		return fmt.Errorf("apphost.hosts on node2 never named node1 for the app within %v of the new link", converge)
	}
	return nil
}

// relayOnly links node2 to node1 through relay, closes every other link
// between them, and returns the relayed link's id once both nodes hold it alone.
func relayOnly(ctx *astral.Context, p pair, relay *relay) (astral.Nonce, error) {
	info, err := newLink(ctx, p.admin2, p.n1.Identity, "tcp:"+relay.Addr())
	if err != nil {
		return 0, fmt.Errorf("linking node2 to node1 through the relay: %w", err)
	}
	relayed := info.ID

	ids, err := linkIDs(ctx, p.admin1, p.n2.Identity)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if id == relayed {
			continue
		}
		if err := closeLink(ctx, p.admin1, id); err != nil {
			return 0, err
		}
	}

	alone := func(ids []astral.Nonce) bool { return len(ids) == 1 && ids[0] == relayed }
	if !waitLinks(ctx, p.admin1, p.n2.Identity, alone) || !waitLinks(ctx, p.admin2, p.n1.Identity, alone) {
		return 0, fmt.Errorf("node1 and node2 did not settle on the relayed link %v alone", relayed)
	}
	return relayed, nil
}

// newLink asks the admin's node to link to identity at endpoint.
func newLink(ctx *astral.Context, admin *astrald.Client, identity, endpoint string) (*nodes.LinkInfo, error) {
	lctx, cancel := ctx.WithTimeout(converge)
	defer cancel()
	ch, err := admin.QueryChannel(lctx, "nodes.new_link", query.Args{"identity": identity, "endpoint": endpoint})
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	var info *nodes.LinkInfo
	if err := ch.Switch(channel.Expect(&info), channel.PassErrors, channel.WithContext(lctx), channel.FailOnEOF()); err != nil {
		return nil, err
	}
	return info, nil
}

func closeLink(ctx *astral.Context, admin *astrald.Client, id astral.Nonce) error {
	ch, err := admin.QueryChannel(ctx, "nodes.close_link", query.Args{"link_id": id.String()})
	if err != nil {
		return fmt.Errorf("nodes.close_link %v: %w", id, err)
	}
	defer ch.Close()
	if err := ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return fmt.Errorf("nodes.close_link %v: %w", id, err)
	}
	return nil
}

// linkIDs returns the ids of the admin's node's links to peer.
func linkIDs(ctx *astral.Context, admin *astrald.Client, peer string) ([]astral.Nonce, error) {
	links, err := nodescli.New(nil, admin).Links(ctx)
	if err != nil {
		return nil, fmt.Errorf("nodes.links: %w", err)
	}
	var ids []astral.Nonce
	for _, l := range links {
		if l.RemoteIdentity.String() == peer {
			ids = append(ids, l.ID)
		}
	}
	return ids, nil
}

// waitLinks polls the admin's node's links to peer until ok holds for their ids.
func waitLinks(ctx *astral.Context, admin *astrald.Client, peer string, ok func([]astral.Nonce) bool) bool {
	deadline := time.Now().Add(converge)
	for time.Now().Before(deadline) {
		if ids, err := linkIDs(ctx, admin, peer); err == nil && ok(ids) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func contains(ids []astral.Nonce, id astral.Nonce) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// knowsHost reports whether apphost.hosts on the admin's node names host for app.
func knowsHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) (bool, error) {
	hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
	found := false
	for h := range hosts {
		found = found || h.String() == host
	}
	if *errp != nil {
		return false, fmt.Errorf("apphost.hosts: %w", *errp)
	}
	return found, nil
}

// waitHost polls apphost.hosts on the admin's node until it names host for app.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(converge)
	for time.Now().Before(deadline) {
		if found, err := knowsHost(ctx, admin, app, host); err == nil && found {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// relay forwards every TCP connection it accepts to target until it is
// black-holed; from then on it drops every byte in both directions and keeps
// every connection open, as a path that vanished without a reset does.
type relay struct {
	ln     net.Listener
	target string
	dead   atomic.Bool
	// lastByte is the unix nanoseconds of the last write that forwarded bytes.
	lastByte atomic.Int64
}

// startRelay listens at host on a free port and forwards to target.
func startRelay(host, target string) (*relay, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, fmt.Errorf("relay listen at %v: %w", host, err)
	}
	r := &relay{ln: ln, target: target}
	r.lastByte.Store(time.Now().UnixNano())
	go r.serve()
	return r, nil
}

func (r *relay) Addr() string { return r.ln.Addr().String() }

func (r *relay) Close() error { return r.ln.Close() }

func (r *relay) blackhole() { r.dead.Store(true) }

// waitQuiet reports whether the relay forwarded no byte for quiet within limit.
func (r *relay) waitQuiet(quiet, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if time.Since(time.Unix(0, r.lastByte.Load())) >= quiet {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (r *relay) serve() {
	for {
		down, err := r.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", r.target)
		if err != nil {
			down.Close()
			continue
		}
		go r.pipe(up, down)
		go r.pipe(down, up)
	}
}

// pipe copies src to dst until the relay is black-holed, then reads src and
// drops what it reads.
// why the close is not passed on once black-holed: a vanished path delivers
// no FIN either.
func (r *relay) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if r.dead.Load() {
			if err != nil {
				return
			}
			continue
		}
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				src.Close()
				return
			}
			r.lastByte.Store(time.Now().UnixNano())
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr, "relay:", err)
			}
			dst.Close()
			return
		}
	}
}
