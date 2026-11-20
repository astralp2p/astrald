package nearby_test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
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

// lanUserConfig is the user.yaml of every node, the admission policies of the E2E nodes
// (tests/lib/nodeconfig.py E2E_USER).
const lanUserConfig = "policies:\n  swarm_join: accept_all\n  swarm_invite: accept_all\n  contract_accept: accept_all\n"

// lanWait bounds each wait of these tests; a link over loopback forms in well under a second.
const lanWait = 20 * time.Second

// lanListeners numbers the in-memory apphost listeners, which share one namespace in a process.
var lanListeners atomic.Uint64

var loopback = ip.IP(net.IPv4(127, 0, 0, 1).To4())

// lanNode is a running node of these tests.
type lanNode struct {
	node *core.Node
	port int // the tcp listen port
}

func (n *lanNode) id() *astral.Identity { return n.node.Identity() }

func (n *lanNode) nearby() *nearbysrc.Module {
	return n.node.Modules().Find("nearby").(*nearbysrc.Module)
}

// user returns a user client of this node that calls as caller.
func (n *lanNode) user(caller *astral.Identity) *userClient.Client {
	return userClient.New(n.id(), core.NewClientAs(n.node, caller))
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

// Ticket g6pAQZTYlY criterion 8: an unclaimed node, in the default Visible mode on a custom
// tcp port, is adopted by its nearby alias without a typed address; the adopter dials the
// port the status carries.
func TestUnclaimedNodeOnACustomPortIsAdoptedThroughNearby(t *testing.T) {
	owner, fresh := runLanNode(t), runLanNode(t)
	userID := owner.bootstrap(t, "").Identity

	s := owner.deliver(t, fresh)
	profile, ok := attachment[*nearby.PublicProfile](s)
	if !ok || profile.NodeAlias == "" {
		t.Fatalf("the status of the unclaimed node holds no public profile with an alias")
	}
	lp, ok := attachment[*nearby.ListenPorts](s)
	if !ok || int(lp.TCP) != fresh.port {
		t.Fatalf("the Visible status carries %+v; want tcp %v", lp, fresh.port)
	}

	want := fmt.Sprintf("tcp:127.0.0.1:%d", fresh.port)
	if got := owner.nodeEndpoints(t, fresh.id()); len(got) != 1 || got[0] != want {
		t.Fatalf("the owner resolves %v for the unclaimed node; want [%v]", got, want)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(lanWait)
	defer cancel()
	signed, err := owner.user(userID).Adopt(ctx, "."+string(profile.NodeAlias))
	if err != nil {
		t.Fatalf("user.adopt .%v: %v", profile.NodeAlias, err)
	}
	if !signed.Subject.IsEqual(fresh.id()) || !signed.Issuer.IsEqual(userID) {
		t.Fatalf("adopt signed %v for %v; want %v for %v", signed.Issuer, signed.Subject, userID, fresh.id())
	}
	// note: the subject signature comes from user.accept_membership on the unclaimed node,
	// which the owner reaches only at the endpoint nearby resolved.
	if signed.SubjectSig == nil {
		t.Fatal("the adopted node did not sign the contract")
	}
	waitFor(t, "the adopted node holds the user's contract", func() bool {
		return contractOf(t, fresh).Issuer.IsEqual(userID)
	})
}

// Ticket 98SVlhAVHt criterion 5: a fresh node runs user.join toward an owned Stealth node on
// the same LAN, with default modes and no endpoint of it; the swarm node's statuses arrive
// while the join waits, the join recognises them with the target's ID, and the reply is a
// signed contract after which user.info names the user.
func TestJoinFindsAStealthSwarmNodeOnTheLAN(t *testing.T) {
	swarm, fresh := runLanNode(t), runLanNode(t)
	userID := swarm.bootstrap(t, "").Identity

	// note: a status before the join is cached by IP only and resolves nothing.
	fresh.deliver(t, swarm)
	if got := fresh.nodeEndpoints(t, swarm.id()); len(got) != 0 {
		t.Fatalf("before the join, the fresh node resolves %v for the swarm node; want none", got)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		deliverEvery(fresh, swarm, stop)
	}()
	defer func() { close(stop); <-done }()

	ctx, cancel := astral.NewContext(nil).WithTimeout(lanWait)
	defer cancel()
	signed, err := fresh.user(fresh.id()).Join(ctx, swarm.id().String())
	if err != nil {
		t.Fatalf("user.join %v: %v", swarm.id(), err)
	}
	if !signed.Issuer.IsEqual(userID) || !signed.Subject.IsEqual(fresh.id()) {
		t.Fatalf("join signed %v for %v; want %v for %v", signed.Issuer, signed.Subject, userID, fresh.id())
	}

	if c := contractOf(t, fresh); !c.Issuer.IsEqual(userID) {
		t.Fatalf("user.info names the user %v; want %v", c.Issuer, userID)
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

	port := freePort(t)
	root := t.TempDir()
	res := lanResources(t, root)

	key := secp256k1.New()
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, key, astral.Canonical()); err != nil {
		t.Fatalf("encode node key: %v", err)
	}
	if err := res.WriteData("node_key", buf.Bytes()); err != nil {
		t.Fatalf("write node key: %v", err)
	}

	endpoint := fmt.Sprintf("memu:nearby-lan-%d", lanListeners.Add(1))
	files := map[string]string{
		"node.yaml":    "modules: [" + lanModules + "]\n",
		"apphost.yaml": fmt.Sprintf("listen: [%q]\nbind_http: \"\"\n", endpoint),
		"ether.yaml":   "listen: false\n",
		"tcp.yaml":     fmt.Sprintf("listen_port: %d\n", port),
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
	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitLanNode(t, ctx, endpoint, done)
	return &lanNode{node: node, port: port}
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
