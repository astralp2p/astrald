package user

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// TestIsSwarmMember: a node with an active membership is a member; an
// expelled node, a stranger and the zero identity are not.
func TestIsSwarmMember(t *testing.T) {
	e := newMemberEnv(t)
	self, sibling, banned, stranger := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	e.query.found = []*auth.SignedContract{
		memberContract(t, e.issuer, self),
		memberContract(t, e.issuer, sibling),
		memberContract(t, e.issuer, banned),
	}
	if err := e.mod.db.StoreExpulsion(sampleSigned(e.issuer, banned)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}

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
	sibling := astral.GenerateIdentity()
	mod := &Module{Deps: Deps{Auth: &memberAuth{query: &memberQuery{found: []*auth.SignedContract{
		memberContract(t, astral.GenerateIdentity(), sibling),
	}}}}}

	if mod.IsSwarmMember(sibling) {
		t.Fatal("an unclaimed node reports a member")
	}
}

// TestIsSwarmMemberFailsClosed: a contract lookup fault reads as no
// membership, even for an identity the index named before.
func TestIsSwarmMemberFailsClosed(t *testing.T) {
	e := newMemberEnv(t)
	e.mod.log = log.New(nil)
	sibling := astral.GenerateIdentity()
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, sibling)}
	e.query.err = errors.New("index unavailable")

	if e.mod.IsSwarmMember(sibling) {
		t.Fatal("IsSwarmMember reported a member while the contract lookup failed")
	}
}

// TestIsSwarmMemberFailsClosedOnBanStoreFault: an unreadable ban store reads
// as no membership, so an expelled node is never reported a member.
func TestIsSwarmMemberFailsClosedOnBanStoreFault(t *testing.T) {
	e := newMemberEnv(t)
	e.mod.log = log.New(nil)
	sibling, banned := astral.GenerateIdentity(), astral.GenerateIdentity()
	e.query.found = []*auth.SignedContract{
		memberContract(t, e.issuer, sibling),
		memberContract(t, e.issuer, banned),
	}
	if err := e.mod.db.StoreExpulsion(sampleSigned(e.issuer, banned)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}
	if e.mod.IsSwarmMember(banned) || !e.mod.IsSwarmMember(sibling) {
		t.Fatal("membership is wrong while the ban store is readable")
	}

	sqlDB, err := e.mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	for name, id := range map[string]*astral.Identity{"an expelled node": banned, "a sibling": sibling} {
		if e.mod.IsSwarmMember(id) {
			t.Errorf("%s reported a member while the ban store cannot be read", name)
		}
	}
	if swarm := e.mod.LocalSwarm(); len(swarm) != 0 {
		t.Errorf("LocalSwarm = %v while the ban store cannot be read, want none", swarm)
	}
}

// BenchmarkIsSwarmMember measures the per-frame cost of the inbound relay
// check on a 10-node swarm with one ban, read from a SwarmView.
func BenchmarkIsSwarmMember(b *testing.B) {
	issuer := astral.GenerateIdentity()
	mod := banModule(b, issuer)
	query := &memberQuery{}
	useQuerySwarm(mod, issuer, query)
	var last *astral.Identity
	for range 10 {
		last = astral.GenerateIdentity()
		query.found = append(query.found, memberContract(b, issuer, last))
	}
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, astral.GenerateIdentity())); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		if !mod.IsSwarmMember(last) {
			b.Fatal("member not found")
		}
	}
}
