package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// swarmUser is a user module whose local swarm is fixed.
type swarmUser struct {
	usermod.Module
	swarm []*astral.Identity
}

func (u *swarmUser) LocalSwarm() []*astral.Identity { return u.swarm }

func TestIsSwarmApp(t *testing.T) {
	self, sibling, friend := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	local, remote, foreign, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self,
		relayContract(local, self),
		relayContract(remote, sibling),
		relayContract(foreign, friend),
		contractFor(other, self, "mod.auth.serve_apps_action"),
	)
	mod.User = &swarmUser{swarm: []*astral.Identity{self, sibling}}
	ctx := astral.NewContext(nil)

	for _, tc := range []struct {
		name string
		app  *astral.Identity
		want bool
	}{
		{"hosted on this node", local, true},
		{"hosted on a sibling", remote, true},
		{"hosted on a friend's node", foreign, false},
		{"no relay contract", other, false},
		{"unknown", astral.GenerateIdentity(), false},
		{"zero", &astral.Identity{}, false},
		{"nil", nil, false},
	} {
		if got := mod.IsSwarmApp(ctx, tc.app); got != tc.want {
			t.Errorf("%s: IsSwarmApp = %v, want %v", tc.name, got, tc.want)
		}
	}

	mod.User = nil
	if mod.IsSwarmApp(ctx, local) {
		t.Error("a node without a user module has a swarm app")
	}
	mod.User = &swarmUser{}
	if mod.IsSwarmApp(ctx, local) {
		t.Error("a node without a swarm has a swarm app")
	}
}

