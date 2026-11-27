package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestIsSwarmMember: a member with an active node contract is a member; an
// expelled node, a stranger and the zero identity are not.
func TestIsSwarmMember(t *testing.T) {
	e := newMemberEnv(t)
	self, sibling, banned, stranger := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	for _, id := range []*astral.Identity{self, sibling, banned} {
		e.add(t, id, time.Hour)
	}
	banNode(t, e.mod, e.issuer, banned)

	for _, c := range []struct {
		name string
		id   *astral.Identity
		want bool
	}{
		{"this node", self, true},
		{"a sibling", sibling, true},
		{"an expelled node", banned, false},
		{"a stranger", stranger, false},
		{"the zero identity", &astral.Identity{}, false},
		{"nil", nil, false},
	} {
		if got := e.mod.IsSwarmMember(c.id); got != c.want {
			t.Errorf("%s: IsSwarmMember = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIsSwarmMemberOnAnUnclaimedNode: a node without an active contract has no
// swarm, so no identity is a member.
func TestIsSwarmMemberOnAnUnclaimedNode(t *testing.T) {
	node, sibling := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := newStoreModule(t, node, nil, nil, time.Now, realRules)
	storeMembership(t, mod, node, sibling, user.TierMember, time.Now().Add(time.Hour))

	if mod.IsSwarmMember(sibling) {
		t.Fatal("an unclaimed node reports a member")
	}
}

// TestIsSwarmMemberFailsClosed: a swarm that cannot be read reports no member.
//
// note: a store that is not open lists no entry; it never held one, because a
// store cannot be closed again to fault a read.
func TestIsSwarmMemberFailsClosed(t *testing.T) {
	issuer := astral.GenerateIdentity()
	mod := claimedModule(t, issuer)
	mod.node = &identityNode{id: astral.GenerateIdentity()}
	mod.swarmState = newSwarmStore(realRules(mod.rootKey))
	mod.swarmView = newSwarmView(mod, mod.rootKey, time.Now)

	if mod.IsSwarmMember(issuer) || mod.IsSwarmMember(mod.node.Identity()) {
		t.Fatal("IsSwarmMember reported a member of a store that is not open")
	}
}

// BenchmarkIsSwarmMember measures the per-frame cost of the inbound relay
// check on a 10-node swarm with one ban, over a real store.
func BenchmarkIsSwarmMember(b *testing.B) {
	issuer := astral.GenerateIdentity()
	mod := newStoreModule(b, astral.GenerateIdentity(), issuer, nil, time.Now, realRules)
	var last *astral.Identity
	for range 10 {
		last = astral.GenerateIdentity()
		storeMembership(b, mod, issuer, last, user.TierMember, time.Now().Add(time.Hour))
	}
	banNode(b, mod, issuer, astral.GenerateIdentity())

	b.ResetTimer()
	for range b.N {
		if !mod.IsSwarmMember(last) {
			b.Fatal("member not found")
		}
	}
}
