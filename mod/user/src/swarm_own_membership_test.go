package user

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// membershipOf builds subject's Member-tier membership issued by issuer,
// signed by a placeholder the scripted checker never verifies.
func membershipOf(t *testing.T, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()
	c, err := user.NewMembership(issuer, subject, user.TierMember, nil, time.Hour)
	if err != nil {
		t.Fatalf("NewMembership: %v", err)
	}
	return &auth.SignedContract{Contract: c, IssuerSig: scriptedSig(), SubjectSig: scriptedSig()}
}

// LinkedIdentities makes linkedNodes the nodes module of the exchange: it holds a link with each of n.linked.
func (n *linkedNodes) LinkedIdentities() []*astral.Identity { return n.linked }

// TestOwnMembershipExchangesSwarmStateWithLinkedPeers: the store accepting
// this node's own membership runs the first-link exchange with every peer the
// node holds a link with, each once; another node's membership and a
// duplicate run none.
//
// why: a node claimed while already linked skipped that exchange, so its
// peers never learned its membership (services-swarm-membership).
func TestOwnMembershipExchangesSwarmStateWithLinkedPeers(t *testing.T) {
	self, issuer := astral.GenerateIdentity(), astral.GenerateIdentity()
	n := &stateNode{id: self, files: newMemFiles(), pushed: &entryPushLog{}}
	n.checker = &scriptedChecker{script: newSwarmScript()}
	n.open(t)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	// note: the exchange offers this node's membership to each peer, which the peer's router sees as a push.
	var mu sync.Mutex
	got := map[string]int{}
	node := &swarmPeerNode{identityNode: identityNode{id: self}, onPush: func(p swarmPush) {
		mu.Lock()
		defer mu.Unlock()
		got[p.target.String()]++
	}}
	mod := &Module{node: node, log: log.New(self), ctx: ctx, now: time.Now}
	mod.Nodes = &linkedNodes{linked: []*astral.Identity{a, b}}
	mod.swarm = n.store
	own := membershipOf(t, issuer, self)
	if err := mod.config.ActiveContract.Set(nil, own); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	mod.swarmView.Store(newSwarmView(mod, mod.rootKey, mod.now))
	exchanged := func() map[string]int {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		out := map[string]int{}
		for k, v := range got {
			out[k] = v
		}
		clear(got)
		return out
	}

	if err := mod.mergeSwarmEntry(ctx, membershipOf(t, issuer, astral.GenerateIdentity())); err != nil {
		t.Fatalf("add another node's membership: %v", err)
	}
	if ex := exchanged(); len(ex) != 0 {
		t.Fatalf("another node's membership exchanged with %v; want none", ex)
	}

	if err := mod.mergeSwarmEntry(ctx, own); err != nil {
		t.Fatalf("add own membership: %v", err)
	}
	if ex := exchanged(); len(ex) != 2 || ex[a.String()] != 1 || ex[b.String()] != 1 {
		t.Fatalf("own membership exchanged %v; want each of %v and %v once", ex, a, b)
	}

	if err := mod.mergeSwarmEntry(ctx, own); err != nil {
		t.Fatalf("add own membership again: %v", err)
	}
	if ex := exchanged(); len(ex) != 0 {
		t.Fatalf("a duplicate exchanged with %v; want none", ex)
	}
}
