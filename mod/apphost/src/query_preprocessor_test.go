package apphost

import (
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// memberUser is a user module of a node claimed by user whose local swarm is
// members; a nil user is an unclaimed node.
type memberUser struct {
	usermod.Module
	user    *astral.Identity
	members []*astral.Identity
}

func (u *memberUser) Identity() *astral.Identity { return u.user }

func (u *memberUser) IsSwarmMember(id *astral.Identity) bool {
	return u.user != nil && slices.ContainsFunc(u.members, id.IsEqual)
}

// relaysOf runs PreprocessQuery for a query from caller to target and returns
// the relay candidates it added, in order.
func relaysOf(t *testing.T, mod *Module, caller, target *astral.Identity) []*astral.Identity {
	t.Helper()
	q := astral.Launch(&astral.Query{Nonce: astral.NewNonce(), Caller: caller, Target: target, QueryString: "ping"})
	if err := mod.PreprocessQuery(core.NewQueryModifier(q)); err != nil {
		t.Fatalf("PreprocessQuery: %v", err)
	}
	v, ok := q.Extra.Get(nodes.ExtraRelayVia)
	if !ok {
		return nil
	}
	relays, ok := v.([]*astral.Identity)
	if !ok {
		t.Fatalf("relay list is %T; want []*astral.Identity", v)
	}
	return relays
}

// TestPreprocessQueryRelaysOnlyThroughSwarmMembers: on a claimed node, an app
// hosted on member A and on former member B is relayed through A alone
// (A2G8, astral-docs topics/app-routing.md, Resolving the host).
func TestPreprocessQueryRelaysOnlyThroughSwarmMembers(t *testing.T) {
	self, a, b, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self, relayContract(app, b), relayContract(app, a), relayContract(app, self))
	mod.User = &memberUser{user: astral.GenerateIdentity(), members: []*astral.Identity{self, a}}

	got := relaysOf(t, mod, self, app)
	if want := sortedStrings(a); !slices.Equal(sortedStrings(got...), want) || len(got) != 1 {
		t.Fatalf("PreprocessQuery added relays %v; want only member %v", got, a)
	}
}

// TestPreprocessQueryOnAnUnclaimedNodeRelaysThroughEveryHost: a node without a
// user has no swarm to filter by, and keeps every host but itself.
func TestPreprocessQueryOnAnUnclaimedNodeRelaysThroughEveryHost(t *testing.T) {
	self, a, b, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	for name, user := range map[string]usermod.Module{
		"no user module":  nil,
		"no active claim": &memberUser{members: []*astral.Identity{a}},
	} {
		mod, _ := hostsModule(self, relayContract(app, a), relayContract(app, b), relayContract(app, self))
		mod.User = user

		got := relaysOf(t, mod, self, app)
		if want := sortedStrings(a, b); !slices.Equal(sortedStrings(got...), want) {
			t.Errorf("%s: PreprocessQuery added relays %v; want %v", name, sortedStrings(got...), want)
		}
	}
}

// TestPreprocessQueryWithNoMemberHostAddsNoRelay: an app whose only host is a
// former member has no relay candidate on a claimed node, so routing ends as
// route not found without a dial.
func TestPreprocessQueryWithNoMemberHostAddsNoRelay(t *testing.T) {
	self, b, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self, relayContract(app, b))
	mod.User = &memberUser{user: astral.GenerateIdentity(), members: []*astral.Identity{self}}

	if got := relaysOf(t, mod, self, app); len(got) != 0 {
		t.Fatalf("PreprocessQuery added relays %v for an app hosted only on a former member; want none", got)
	}
}
