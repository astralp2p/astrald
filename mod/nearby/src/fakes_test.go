package nearby

import (
	"bytes"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	ethermod "github.com/astralp2p/astrald/mod/ether"
	eventsmod "github.com/astralp2p/astrald/mod/events"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
	secp256k1mod "github.com/astralp2p/astrald/mod/secp256k1"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Fakes and builders shared by the nearby tests.
//
// why: each fake embeds the nil module interface it stands in for, so a call to any
// method the fake does not implement panics, which is the assertion that the code under
// test uses nothing else.

// waitTimeout bounds every wait for an asynchronous effect.
const waitTimeout = 5 * time.Second

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// testUser is a user module whose user is id, nil when the node has no owner.
type testUser struct {
	usermod.Module
	id *astral.Identity
}

func (u testUser) Identity() *astral.Identity { return u.id }

func (u testUser) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

// subnetSend is one PushToSubnetOf call.
type subnetSend struct {
	src ip.IP
	obj astral.Object
}

// fakeEther records what the module sends. Its local subnets are subnets.
type fakeEther struct {
	ethermod.Module
	subnets []*net.IPNet

	mu        sync.Mutex
	pushed    []astral.Object
	subnetted []subnetSend
	sends     chan struct{}
}

func newFakeEther(cidrs ...string) *fakeEther {
	e := &fakeEther{sends: make(chan struct{}, 1024)}
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		e.subnets = append(e.subnets, n)
	}
	return e
}

func (e *fakeEther) Push(obj astral.Object) error {
	e.mu.Lock()
	e.pushed = append(e.pushed, obj)
	e.mu.Unlock()
	e.sends <- struct{}{}
	return nil
}

func (e *fakeEther) PushToSubnetOf(src ip.IP, obj astral.Object) error {
	e.mu.Lock()
	e.subnetted = append(e.subnetted, subnetSend{src: src, obj: obj})
	e.mu.Unlock()
	e.sends <- struct{}{}
	return nil
}

func (e *fakeEther) SubnetOf(src ip.IP) (string, bool) {
	for _, n := range e.subnets {
		if n.Contains(net.IP(src)) {
			return n.String(), true
		}
	}
	return "", false
}

func (e *fakeEther) recorded() (pushed []astral.Object, subnetted []subnetSend) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.pushed), slices.Clone(e.subnetted)
}

// waitSend waits for one more send of any kind.
func (e *fakeEther) waitSend(t *testing.T) {
	t.Helper()
	select {
	case <-e.sends:
	case <-time.After(waitTimeout):
		t.Fatal("no datagram was sent")
	}
}

// requireNoSend fails when a send happens within a short grace period.
func (e *fakeEther) requireNoSend(t *testing.T) {
	t.Helper()
	select {
	case <-e.sends:
		pushed, subnetted := e.recorded()
		t.Fatalf("sent a datagram: %d broadcasts, %d subnet broadcasts; want none", len(pushed), len(subnetted))
	case <-time.After(50 * time.Millisecond):
	}
}

// fakeCrypto signs with key through the secp256k1 module's ASN.1 signer and verifies
// through its ASN.1 verifier.
type fakeCrypto struct {
	cryptomod.Module
	key *secp256k1.PrivateKey

	signers  atomic.Uint64
	verifies atomic.Uint64
}

func (c *fakeCrypto) NodeSigner() cryptomod.HashSigner {
	c.signers.Add(1)
	return secp256k1mod.NewHashSignerASN1(privateKey(c.key))
}

func (c *fakeCrypto) VerifyHashSignature(key *crypto.PublicKey, sig *crypto.Signature, hash []byte) error {
	c.verifies.Add(1)
	return secp256k1mod.VerifyASN1(key, hash, sig)
}

func privateKey(key *secp256k1.PrivateKey) *crypto.PrivateKey {
	return &crypto.PrivateKey{Type: secp256k1api.KeyType, Key: key.Serialize()}
}

// fakeEvents records the event data the module emits.
type fakeEvents struct {
	eventsmod.Module

	mu      sync.Mutex
	emitted []astral.Object
}

func (e *fakeEvents) Emit(data astral.Object) *events.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, data)
	return &events.Event{ID: astral.NewNonce(), Data: data}
}

func (e *fakeEvents) peersSeen(t *testing.T) []*nearby.PeerSeen {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()

	var list []*nearby.PeerSeen
	for _, d := range e.emitted {
		seen, ok := d.(*nearby.PeerSeen)
		if !ok {
			t.Fatalf("emitted %T; want only *nearby.PeerSeen", d)
		}
		list = append(list, seen)
	}
	return list
}

// fakeRecognizer answers RecognizeStatus with recognize.
type fakeRecognizer struct {
	recognize func(*nearby.StatusMessage) *nearbymod.Recognition
	calls     atomic.Uint64
}

func (r *fakeRecognizer) RecognizeStatus(s *nearby.StatusMessage) *nearbymod.Recognition {
	r.calls.Add(1)
	return r.recognize(s)
}

// userRecognizer recognizes user's nodes as the user module does: a stealth hint that
// opens with user, and a public profile whose UserID is user.
func userRecognizer(user *astral.Identity) *fakeRecognizer {
	return &fakeRecognizer{recognize: func(s *nearby.StatusMessage) *nearbymod.Recognition {
		objs := s.Attachments.Objects()
		if hint, ok := astral.First[*nearby.StealthHint](objs); ok {
			if node, ok := hint.Open(user); ok {
				return &nearbymod.Recognition{NodeID: node, UserID: user}
			}
			return nil
		}
		if p, ok := astral.First[*nearby.PublicProfile](objs); ok && p.UserID.IsEqual(user) && !user.IsZero() {
			return &nearbymod.Recognition{NodeID: p.NodeID, UserID: user}
		}
		return nil
	}}
}

// fixedRecognizer recognizes every status as node of user.
func fixedRecognizer(node, user *astral.Identity) *fakeRecognizer {
	return &fakeRecognizer{recognize: func(*nearby.StatusMessage) *nearbymod.Recognition {
		return &nearbymod.Recognition{NodeID: node, UserID: user}
	}}
}

// modeComposer attaches what the user module attaches in each mode: the adoptable flag
// and a profile without a user while unowned, a profile with the user when owned and
// Visible, and a stealth hint when owned and Stealth.
type modeComposer struct {
	mod   *Module
	owner *astral.Identity
	alias string
	calls atomic.Uint64
}

func (c *modeComposer) ComposeStatus(comp nearbymod.Composition) {
	c.calls.Add(1)
	node := c.mod.node.Identity()
	switch c.mod.Mode() {
	case nearby.ModeVisible:
		if c.owner == nil {
			_ = comp.Attach(nearby.NewFlag("adoptable"))
		}
		_ = comp.Attach(&nearby.PublicProfile{NodeID: node, UserID: c.owner, NodeAlias: astral.String8(c.alias)})
	case nearby.ModeStealth:
		if c.owner == nil {
			return
		}
		hint, err := nearby.NewStealthHint(node, c.owner, astral.NewNonce())
		if err == nil {
			_ = comp.Attach(hint)
		}
	}
}

// fakeClock is the module's time: now reads t, and every after call queues a wait the
// test fires.
type fakeClock struct {
	mu    sync.Mutex
	t     time.Time
	waits chan *fakeWait
}

// fakeWait is one pending after call.
type fakeWait struct {
	d  time.Duration
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC), waits: make(chan *fakeWait, 64)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

func (c *fakeClock) after(d time.Duration) <-chan time.Time {
	w := &fakeWait{d: d, ch: make(chan time.Time, 1)}
	c.waits <- w
	return w.ch
}

// nextWait returns the next wait the module starts.
func (c *fakeClock) nextWait(t *testing.T) *fakeWait {
	t.Helper()
	select {
	case w := <-c.waits:
		return w
	case <-time.After(waitTimeout):
		t.Fatal("the module started no wait")
		return nil
	}
}

// requireNoWait fails when the module starts a wait within a short grace period.
func (c *fakeClock) requireNoWait(t *testing.T) {
	t.Helper()
	select {
	case w := <-c.waits:
		t.Fatalf("the module started a wait of %v; want none", w.d)
	case <-time.After(50 * time.Millisecond):
	}
}

func (w *fakeWait) fire(at time.Time) { w.ch <- at }

// harness is a module wired to fakes. self is the local node and clock its time.
type harness struct {
	mod    *Module
	self   *astral.Identity
	ether  *fakeEther
	crypto *fakeCrypto
	events *fakeEvents
	clock  *fakeClock
}

// newHarness returns a module whose node is owned by owner, unowned when owner is nil.
// Its ether has the local subnets 192.168.1.0/24 and 10.0.0.0/24.
func newHarness(t *testing.T, owner *astral.Identity) *harness {
	t.Helper()
	key := generateKey(t)
	self := astral.IdentityFromPubKey(key.PubKey())

	h := &harness{
		self:   self,
		ether:  newFakeEther("192.168.1.0/24", "10.0.0.0/24"),
		crypto: &fakeCrypto{key: key},
		events: &fakeEvents{},
		clock:  newFakeClock(),
	}
	h.mod = &Module{
		Deps:  Deps{Crypto: h.crypto, Ether: h.ether, Events: h.events, User: testUser{id: owner}},
		node:  &identityNode{id: self},
		log:   log.New(self),
		now:   h.clock.now,
		after: h.clock.after,
	}
	return h
}

func (h *harness) setMode(t *testing.T, m nearby.Mode) {
	t.Helper()
	if err := h.mod.SetMode(astral.NewContext(nil), m); err != nil {
		t.Fatalf("SetMode(%v): %v", m, err)
	}
}

func (h *harness) setSilentListen(t *testing.T, v bool) {
	t.Helper()
	b := astral.Bool(v)
	if err := h.mod.silentListen.Set(astral.NewContext(nil), &b); err != nil {
		t.Fatalf("set silent_listen: %v", err)
	}
}

// cached returns the entry stored for src, live or not.
func (h *harness) cached(src string) (*entry, bool) {
	h.mod.cache.mu.Lock()
	defer h.mod.cache.mu.Unlock()
	e, ok := h.mod.cache.entries[ipOf(src).String()]
	return e, ok
}

func (h *harness) cacheLen() int {
	h.mod.cache.mu.Lock()
	defer h.mod.cache.mu.Unlock()
	return len(h.mod.cache.entries)
}

// requireNothingStored fails when the cache holds an entry or an event was emitted.
func (h *harness) requireNothingStored(t *testing.T) {
	t.Helper()
	if n := h.cacheLen(); n != 0 {
		t.Errorf("cache holds %d entries; want none", n)
	}
	if seen := h.events.peersSeen(t); len(seen) != 0 {
		t.Errorf("emitted %d PeerSeen; want none", len(seen))
	}
}

func generateKey(t *testing.T) *secp256k1.PrivateKey {
	t.Helper()
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func ipOf(s string) ip.IP {
	return ip.IP(net.ParseIP(s).To4())
}

// peer is a remote node that signs its statuses as nearby does.
type peer struct {
	key  *secp256k1.PrivateKey
	node *astral.Identity
}

func newPeer(t *testing.T) *peer {
	t.Helper()
	key := generateKey(t)
	return &peer{key: key, node: astral.IdentityFromPubKey(key.PubKey())}
}

// proof returns the StatusProof of a status made of others, stamped ts and wrapped
// when stealth.
func (p *peer) proof(t *testing.T, ts time.Time, stealth bool, others ...astral.Object) *nearby.StatusProof {
	t.Helper()
	nonce := astral.NewNonce()
	hash, err := nearby.ProofHash(others, nonce, astral.Time(ts))
	if err != nil {
		t.Fatalf("ProofHash: %v", err)
	}
	sig, err := secp256k1mod.SignASN1(privateKey(p.key), hash)
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}

	proof := &nearby.StatusProof{Nonce: nonce, Timestamp: astral.Time(ts), Sig: astral.Bytes8(sig.Data)}
	if stealth {
		proof.Sig = nearby.WrapProofSig(p.node, nonce, sig.Data)
	}
	return proof
}

// status returns others followed by their proof.
func (p *peer) status(t *testing.T, ts time.Time, stealth bool, others ...astral.Object) *nearby.StatusMessage {
	t.Helper()
	return statusOf(t, slices.Concat(others, []astral.Object{p.proof(t, ts, stealth, others...)})...)
}

// visible returns the peer's Visible status naming user and alias.
func (p *peer) visible(t *testing.T, ts time.Time, user *astral.Identity, alias string) *nearby.StatusMessage {
	t.Helper()
	return p.status(t, ts, false, &nearby.PublicProfile{NodeID: p.node, UserID: user, NodeAlias: astral.String8(alias)})
}

// stealth returns the peer's Stealth status with a hint for user.
func (p *peer) stealth(t *testing.T, ts time.Time, user *astral.Identity) *nearby.StatusMessage {
	t.Helper()
	return p.status(t, ts, true, stealthHint(t, p.node, user))
}

func stealthHint(t *testing.T, node, user *astral.Identity) *nearby.StealthHint {
	t.Helper()
	hint, err := nearby.NewStealthHint(node, user, astral.NewNonce())
	if err != nil {
		t.Fatalf("NewStealthHint: %v", err)
	}
	return hint
}

// statusOf returns a status holding objs in order.
func statusOf(t *testing.T, objs ...astral.Object) *nearby.StatusMessage {
	t.Helper()
	bundle := astral.NewBundle()
	if err := bundle.Append(objs...); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return &nearby.StatusMessage{Attachments: bundle}
}

// memNode is an in-memory tree.Node holding one value and named sub-nodes.
type memNode struct {
	mu    sync.Mutex
	value astral.Object
	subs  map[string]*memNode
}

var _ tree.Node = &memNode{}

func newMemNode() *memNode {
	return &memNode{value: &astral.Nil{}, subs: map[string]*memNode{}}
}

// note: follow is ignored; the channel carries the current value and closes.
func (n *memNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ch := make(chan astral.Object, 1)
	ch <- n.value
	close(ch)
	return ch, nil
}

func (n *memNode) Set(_ *astral.Context, object astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.value = object
	return nil
}

func (n *memNode) Delete(*astral.Context) error { return errors.New("not supported") }

func (n *memNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	subs := make(map[string]tree.Node, len(n.subs))
	for name, sub := range n.subs {
		subs[name] = sub
	}
	return subs, nil
}

func (n *memNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.subs[name]; ok {
		return nil, tree.ErrAlreadyExists
	}
	sub := newMemNode()
	n.subs[name] = sub
	return sub, nil
}

// at returns the node at path below n, creating the missing nodes.
func (n *memNode) at(t *testing.T, path string) *memNode {
	t.Helper()
	node, err := tree.Query(astral.NewContext(nil), n, path, true)
	if err != nil {
		t.Fatalf("query %v: %v", path, err)
	}
	return node.(*memNode)
}

// answers waits for the op to close the connection and decodes every object it wrote.
func (w *recordingWriter) answers(t *testing.T) []astral.Object {
	t.Helper()
	select {
	case <-w.closed:
	case <-time.After(waitTimeout):
		t.Fatal("the op never closed the connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	r := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	var list []astral.Object
	for {
		obj, err := r.Receive()
		if errors.Is(err, io.EOF) {
			return list
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		list = append(list, obj)
	}
}

// recordingDrop is an objects.Drop that records its Accept calls.
type recordingDrop struct {
	sender *astral.Identity
	object astral.Object

	mu      sync.Mutex
	accepts []bool
}

func (d *recordingDrop) SenderID() *astral.Identity { return d.sender }
func (d *recordingDrop) Object() astral.Object      { return d.object }

func (d *recordingDrop) Accept(save bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accepts = append(d.accepts, save)
	return nil
}

func (d *recordingDrop) accepted() []bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.accepts)
}
