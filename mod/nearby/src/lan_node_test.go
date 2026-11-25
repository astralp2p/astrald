package nearby_test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	userClient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	apphostclient "github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astrald/core"
	_ "github.com/astralp2p/astrald/mod/all"
	nearbysrc "github.com/astralp2p/astrald/mod/nearby/src"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/resources"
)

// note: these tests run whole nodes in one process, wired by the module manager as astrald
// wires them, with tcp listening on custom ports. Ether drops the datagrams of its own host,
// so a test carries each status from one node's nearby to another's (export_test.go) with
// the loopback address as its source IP. Everything after the receive checks is the nodes'
// own: recognition, the port unseal, the endpoint resolver, PeerSeen, the user module's
// contact, the tcp dial and the link.

// lanModules is the module set of a node in these tests: tcp, no kcp.
const lanModules = "apphost, auth, bip137sig, crypto, dir, ether, events, exonet, fs, ip, nearby, nodes, " +
	"objects, scheduler, secp256k1, shell, sys, tcp, tree, user, vfs"

// lanUserConfig is the user.yaml of every node: the defaults.
const lanUserConfig = "{}\n"

// lanWait bounds each wait of these tests; a link over loopback forms in well under a second.
const lanWait = 20 * time.Second

// lanListeners numbers the in-memory apphost listeners, which share one namespace in a process.
var lanListeners atomic.Uint64

var loopback = ip.IP(net.IPv4(127, 0, 0, 1).To4())

// lanNode is a running node of these tests.
type lanNode struct {
	node *core.Node
	port int // the tcp listen port
	root string
	key  *crypto.PrivateKey
	tcp  string // note: extra tcp.yaml lines
	stop func() // note: ends the node and waits for it; safe to call twice
}

func (n *lanNode) id() *astral.Identity { return n.node.Identity() }

func (n *lanNode) nearby() *nearbysrc.Module {
	return n.node.Modules().Find("nearby").(*nearbysrc.Module)
}

// user returns a user client of this node that calls as caller.
func (n *lanNode) user(caller *astral.Identity) *userClient.Client {
	return userClient.New(n.id(), core.NewClientAs(n.node, caller))
}

// inSwarm reports whether n counts peer as a member of its local swarm, which is when its
// link task for peer runs.
func (n *lanNode) inSwarm(peer *astral.Identity) bool {
	swarm := n.node.Modules().Find("user").(interface{ LocalSwarm() []*astral.Identity }).LocalSwarm()
	return slices.ContainsFunc(swarm, peer.IsEqual)
}

// stopLinkRetryTicks makes every link task of n wait its backoff forever, so only a wake relinks.
func (n *lanNode) stopLinkRetryTicks() {
	n.node.Modules().Find("user").(interface {
		SetLinkRetryClock(func(time.Duration) <-chan time.Time)
	}).SetLinkRetryClock(func(time.Duration) <-chan time.Time { return nil })
}

func (n *lanNode) isLinked(peer *astral.Identity) bool {
	return n.node.Modules().Find("nodes").(interface{ IsLinked(*astral.Identity) bool }).IsLinked(peer)
}

// endpoints returns the endpoints of peer that the module resolves, as network:address.
func endpoints(t *testing.T, r interface {
	ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error)
}, peer *astral.Identity) []string {
	t.Helper()
	ch, err := r.ResolveEndpoints(astral.NewContext(nil), peer)
	if err != nil {
		t.Fatalf("resolve endpoints: %v", err)
	}
	var out []string
	for e := range ch {
		out = append(out, e.Endpoint.Network()+":"+e.Endpoint.Address())
	}
	return out
}

func (n *lanNode) nodeEndpoints(t *testing.T, peer *astral.Identity) []string {
	t.Helper()
	return endpoints(t, n.node.Modules().Find("nodes").(interface {
		ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error)
	}), peer)
}

// deliver carries one status that from composes now to n, from the loopback address.
func (n *lanNode) deliver(t *testing.T, from *lanNode) *nearby.StatusMessage {
	t.Helper()
	s, err := from.nearby().ComposeStatus()
	if err != nil || s == nil {
		t.Fatalf("compose the status of %v: %v, %v", from.id(), s, err)
	}
	if err := n.nearby().DeliverStatus(loopback, s); err != nil {
		t.Fatalf("deliver the status of %v: %v", from.id(), err)
	}
	return s
}

// bootstrap claims n with user.bootstrap as the node itself: generated words when words is
// empty, else the given words.
func (n *lanNode) bootstrap(t *testing.T, words string) *user.BootstrapResult {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithTimeout(lanWait)
	defer cancel()
	res, err := n.user(n.id()).Bootstrap(ctx, words, "")
	if err != nil {
		t.Fatalf("user.bootstrap on %v: %v", n.id(), err)
	}
	return res
}

// waitFor polls cond until it holds or lanWait passes.
func waitFor(t *testing.T, what string, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < lanWait {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%v: not within %v", what, lanWait)
	return 0
}

func attachment[T astral.Object](s *nearby.StatusMessage) (T, bool) {
	return astral.First[T](s.Attachments.Objects())
}

// Ticket g6pAQZTYlY criterion 7: two nodes of one User, both claimed with user.bootstrap on
// fresh nodes and so in the owned default Stealth mode, each on a custom tcp port and with no
// endpoint of the other, link from one status of the first: the receiver recognises it,
// unseals its port, resolves it and dials it.
func TestSiblingsOnCustomPortsLinkFromOneStealthStatus(t *testing.T) {
	a, b := runLanNode(t), runLanNode(t)
	words := string(a.bootstrap(t, "").Mnemonic)
	b.bootstrap(t, words)

	if got := b.nodeEndpoints(t, a.id()); len(got) != 0 {
		t.Fatalf("before the status, b resolves %v for a; want none", got)
	}

	s := b.deliver(t, a)
	if _, ok := attachment[*nearby.StealthHint](s); !ok {
		t.Fatal("the status of the claimed node a holds no stealth hint")
	}
	if _, ok := attachment[*nearby.SealedPorts](s); !ok {
		t.Fatal("the status of a on a custom port holds no sealed ports")
	}
	if _, ok := attachment[*nearby.ListenPorts](s); ok {
		t.Fatal("the stealth status of a holds plain listen ports")
	}

	want := fmt.Sprintf("tcp:127.0.0.1:%d", a.port)
	if got := endpoints(t, b.nearby(), a.id()); len(got) != 1 || got[0] != want {
		t.Fatalf("b's nearby resolves %v for a; want [%v]", got, want)
	}

	took := waitFor(t, "b links to a", func() bool { return b.isLinked(a.id()) })
	t.Logf("linked %v after the status", took)
}

// peerSeenLog records the PeerSeen events a node emits, as the user module receives them.
type peerSeenLog struct {
	mu   sync.Mutex
	seen []*nearby.PeerSeen
}

func (l *peerSeenLog) ReceiveObject(drop objects.Drop) error {
	if e, ok := drop.Object().(*events.Event); ok {
		if seen, ok := e.Data.(*nearby.PeerSeen); ok {
			l.mu.Lock()
			l.seen = append(l.seen, seen)
			l.mu.Unlock()
		}
	}
	return nil
}

// of returns the PeerSeen events recorded for node.
func (l *peerSeenLog) of(node *astral.Identity) []*nearby.PeerSeen {
	l.mu.Lock()
	defer l.mu.Unlock()

	var list []*nearby.PeerSeen
	for _, s := range l.seen {
		if s.NodeID.IsEqual(node) {
			list = append(list, s)
		}
	}
	return list
}

// logPeersSeen records the PeerSeen events of n from now on.
func (n *lanNode) logPeersSeen(t *testing.T) *peerSeenLog {
	t.Helper()
	log := &peerSeenLog{}
	if err := n.node.Modules().Find("objects").(objects.Module).AddReceiver(log); err != nil {
		t.Fatalf("add receiver: %v", err)
	}
	return log
}

// Ticket iKytI7lzu1 criterion 4: a node restarted on a new tcp port at the same IP resolves to
// the new port from its next status, which emits one PeerSeen, and its sibling links to it
// there without a link-task tick. A second status of the restarted node emits none.
//
// note: the sibling's link-task backoff is stopped before the restart, so only a wake can
// relink. a never dials (tcp dial off), so only b's task can link them again.
func TestNodeRestartedOnANewPortIsResolvedAfterItsNextStatus(t *testing.T) {
	a, b := runListenOnlyLanNode(t), runLanNode(t)
	words := string(a.bootstrap(t, "").Mnemonic)
	b.bootstrap(t, words)
	seen := b.logPeersSeen(t)

	oldPort := a.port
	b.deliver(t, a)
	waitFor(t, "b links to a", func() bool { return b.isLinked(a.id()) })
	waitFor(t, "b emits PeerSeen for a", func() bool { return len(seen.of(a.id())) == 1 })
	// note: a restart before b took a into its swarm would leave b without a link task for a.
	waitFor(t, "a is in b's swarm", func() bool { return b.inSwarm(a.id()) })
	// why: with b's link-task backoff stopped, a relink can come only from a wake, so the
	// relink below proves the status woke linking, not a link-task tick.
	b.stopLinkRetryTicks()

	a = restartLanNode(t, a)
	if a.port == oldPort {
		t.Fatalf("a restarted on port %v again", oldPort)
	}
	waitFor(t, "b's link to the old a closes", func() bool { return !b.isLinked(a.id()) })

	// note: until a status arrives, nearby still resolves the port a listened on.
	if got, want := endpoints(t, b.nearby(), a.id()), fmt.Sprintf("tcp:127.0.0.1:%d", oldPort); len(got) != 1 || got[0] != want {
		t.Fatalf("before the status, b's nearby resolves %v for a; want [%v]", got, want)
	}

	// note: a tick would have relinked b within linkRetryMin (1 s) of the failed attempt.
	time.Sleep(1500 * time.Millisecond)
	if b.isLinked(a.id()) {
		t.Fatal("b relinked to the restarted a before its status, with link-task ticks stopped")
	}

	delivered := time.Now()
	b.deliver(t, a)
	want := fmt.Sprintf("tcp:127.0.0.1:%d", a.port)
	if got := endpoints(t, b.nearby(), a.id()); len(got) != 1 || got[0] != want {
		t.Fatalf("after the status, b's nearby resolves %v for a; want [%v]", got, want)
	}
	waitFor(t, "b emits PeerSeen for the port change", func() bool { return len(seen.of(a.id())) == 2 })
	waitFor(t, "b links to the restarted a", func() bool { return b.isLinked(a.id()) })
	t.Logf("relinked %v after the status", time.Since(delivered))

	// note: PeerSeen is emitted on its own goroutine, so an event the second status wrongly
	// caused needs a moment to show.
	b.deliver(t, a)
	time.Sleep(300 * time.Millisecond)
	if n := len(seen.of(a.id())); n != 2 {
		t.Fatalf("b emitted %d PeerSeen for a after a second status on the same port; want 2 in all", n)
	}
	last := seen.of(a.id())[1]
	if !bytes.Equal(last.SourceIP, loopback) {
		t.Fatalf("the PeerSeen of the port change names %v; want %v", last.SourceIP, loopback)
	}
}

// deliverEvery carries a fresh status of from to n every 100 ms until stop closes, as
// periodic statuses and scan replies would.
func deliverEvery(n, from *lanNode, stop <-chan struct{}) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			if s, err := from.nearby().ComposeStatus(); err == nil && s != nil {
				_ = n.nearby().DeliverStatus(loopback, s)
			}
		}
	}
}

// contractOf returns the contract user.info shows on n, or an empty one.
func contractOf(t *testing.T, n *lanNode) *auth.SignedContract {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithTimeout(lanWait)
	defer cancel()
	info, err := n.user(n.id()).Info(ctx)
	if err != nil || info.Contract == nil || info.Contract.Contract == nil {
		return &auth.SignedContract{Contract: &auth.Contract{}}
	}
	return info.Contract
}

// runLanNode runs a fresh node with tcp on a free port. The node stops when the test ends.
func runLanNode(t *testing.T) *lanNode {
	t.Helper()
	return runLanNodeWith(t, "")
}

// runListenOnlyLanNode runs a fresh node whose tcp does not dial: it never links out, so every
// link of it is dialed by its peer.
func runListenOnlyLanNode(t *testing.T) *lanNode {
	t.Helper()
	return runLanNodeWith(t, "dial: false\n")
}

// runLanNodeWith runs a fresh node with tcp on a free port and the extra lines of tcp.yaml in
// tcp.
func runLanNodeWith(t *testing.T, tcp string) *lanNode {
	t.Helper()

	root := t.TempDir()
	key := secp256k1.New()
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, key, astral.Canonical()); err != nil {
		t.Fatalf("encode node key: %v", err)
	}
	res := lanResources(t, root)
	if err := res.WriteData("node_key", buf.Bytes()); err != nil {
		t.Fatalf("write node key: %v", err)
	}

	return startLanNode(t, root, key, freePort(t), tcp)
}

// restartLanNode stops n and runs its node again, with the same key and data, on a free port
// other than its old one. The new node stops when the test ends.
func restartLanNode(t *testing.T, n *lanNode) *lanNode {
	t.Helper()

	n.stop()
	port := freePort(t)
	for port == n.port {
		port = freePort(t)
	}
	return startLanNode(t, n.root, n.key, port, n.tcp)
}

// startLanNode runs the node of key, whose config is in root and data in root/data, with tcp
// on port and the extra lines of tcp.yaml in tcp.
func startLanNode(t *testing.T, root string, key *crypto.PrivateKey, port int, tcp string) *lanNode {
	t.Helper()

	res := lanResources(t, root)

	endpoint := fmt.Sprintf("memu:nearby-lan-%d", lanListeners.Add(1))
	files := map[string]string{
		"node.yaml":    "modules: [" + lanModules + "]\n",
		"apphost.yaml": fmt.Sprintf("listen: [%q]\nbind_http: \"\"\n", endpoint),
		"ether.yaml":   "listen: false\n",
		"tcp.yaml":     fmt.Sprintf("listen_port: %d\n", port) + tcp,
		"user.yaml":    lanUserConfig,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatalf("write %v: %v", name, err)
		}
	}

	node, err := core.NewNode(secp256k1.Identity(secp256k1.PublicKey(key)), res)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
	t.Cleanup(stop)

	waitLanNode(t, ctx, endpoint, done)
	return &lanNode{node: node, port: port, root: root, key: key, tcp: tcp, stop: stop}
}

// freePort returns a tcp port no listener holds now, never a default port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitLanNode waits until apphost accepts a session.
// why apphost: modules run only after every module loaded its dependencies.
func waitLanNode(t *testing.T, ctx *astral.Context, endpoint string, done <-chan error) {
	t.Helper()

	deadline := time.Now().Add(lanWait)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("the node ended: %v", err)
		default:
		}

		host, err := apphostclient.Connect(ctx, endpoint)
		if err == nil {
			host.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the node did not run")
}

// lanResources returns the resources of a node: the config in root, the data in root/data.
func lanResources(t *testing.T, root string) *resources.FileResources {
	t.Helper()

	res, err := resources.NewFileResources(root, false)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	dataRoot := filepath.Join(root, "data")
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := res.SetDataRoot(dataRoot); err != nil {
		t.Fatalf("data root: %v", err)
	}
	return res
}
