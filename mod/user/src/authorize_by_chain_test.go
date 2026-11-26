package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// chainFixture: a claimed node whose swarm holds a Privileged, a Trusted and a
// Member node, and a Privileged membership another key signed.
type chainFixture struct {
	rules                                          *chainRules
	user, node, privileged, trusted, member, rogue *astral.Identity
}

func newChainFixture(t *testing.T) *chainFixture {
	f := &chainFixture{
		user: astral.GenerateIdentity(), node: astral.GenerateIdentity(),
		privileged: astral.GenerateIdentity(), trusted: astral.GenerateIdentity(),
		member: astral.GenerateIdentity(), rogue: astral.GenerateIdentity(),
	}
	set := &fakeSwarmSet{members: map[string]*user.Membership{}}
	set.setMember(f.user, f.privileged, user.TierPrivilegedMember)
	set.setMember(f.user, f.trusted, user.TierTrusted)
	set.setMember(f.privileged, f.member, user.TierMember)
	set.setMember(f.privileged, f.rogue, user.TierPrivilegedMember)

	mod := claimedModule(t, f.user)
	mod.node = &identityNode{id: f.node}
	f.rules = newChainRules(mod, set)
	return f
}

// TestChainRelayForIsPrivilegedOnly: only a root-signed Privileged node relays
// as the user; a Member or Trusted node no longer does (D5, D6).
func TestChainRelayForIsPrivilegedOnly(t *testing.T) {
	f := newChainFixture(t)
	relay := func(actor, forID *astral.Identity) *nodes.RelayForAction {
		return &nodes.RelayForAction{Action: auth.NewAction(actor), ForID: forID}
	}
	for _, c := range []struct {
		name string
		a    *nodes.RelayForAction
		want bool
	}{
		{"privileged, for the user", relay(f.privileged, f.user), true},
		{"privileged, for another identity", relay(f.privileged, astral.GenerateIdentity()), false},
		{"trusted", relay(f.trusted, f.user), false},
		{"member", relay(f.member, f.user), false},
		{"privileged membership not signed by the user", relay(f.rogue, f.user), false},
		{"zero actor", relay(&astral.Identity{}, f.user), false},
	} {
		if got := f.rules.AuthorizeRelayFor(nil, c.a); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestChainServiceDiscoveryDropsTheSwarmMemberRule: the user discovers; a swarm
// member of any tier, and this node, discover only through a contract chain.
func TestChainServiceDiscoveryDropsTheSwarmMemberRule(t *testing.T) {
	f := newChainFixture(t)
	for name, c := range map[string]struct {
		actor *astral.Identity
		want  bool
	}{
		"user":       {f.user, true},
		"privileged": {f.privileged, false},
		"member":     {f.member, false},
		"this node":  {f.node, false},
		"zero":       {&astral.Identity{}, false},
	} {
		a := discoveryAction(c.actor, "player")
		a.NodeID = f.node
		if got := f.rules.AuthorizeServiceDiscovery(nil, a); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

// TestChainSeeObjectsDropsTheSwarmMemberRule: the user and this node read; a
// swarm member reads only through a contract chain.
func TestChainSeeObjectsDropsTheSwarmMemberRule(t *testing.T) {
	f := newChainFixture(t)
	for name, c := range map[string]struct {
		actor *astral.Identity
		want  bool
	}{
		"user":       {f.user, true},
		"this node":  {f.node, true},
		"privileged": {f.privileged, false},
		"member":     {f.member, false},
	} {
		if got := f.rules.AuthorizeSeeObjects(nil, &auth.SeeObjectsAction{Action: auth.NewAction(c.actor)}); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

func discoveryAction(actor *astral.Identity, service astral.String8) *services.ServiceDiscoveryAction {
	return &services.ServiceDiscoveryAction{Action: auth.NewAction(actor), Service: service}
}
