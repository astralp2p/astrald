package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

func relayForAction(actor, forID *astral.Identity) *nodes.RelayForAction {
	return &nodes.RelayForAction{Action: auth.NewAction(actor), ForID: forID}
}

func TestAuthorizeRelayForOnUnclaimedNode(t *testing.T) {
	userID, sib := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{Deps: Deps{Auth: &adminNetworkContracts{contracts: []*auth.SignedContract{
		adminNetworkMembership(userID, sib),
	}}}}

	cases := []struct {
		name  string
		forID *astral.Identity
	}{
		{"for the user", userID},
		{"for a zero identity", &astral.Identity{}},
	}

	for _, c := range cases {
		if mod.AuthorizeRelayFor(nil, relayForAction(sib, c.forID)) {
			t.Errorf("%s: authorized true, want false", c.name)
		}
	}
}

func TestAuthorizeRelayForAndSeeSwarmOnClaimedNode(t *testing.T) {
	userID, sib := astral.GenerateIdentity(), astral.GenerateIdentity()
	outsider, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()

	mod := banModule(t, userID)
	mod.Deps.Auth = &adminNetworkContracts{contracts: []*auth.SignedContract{
		adminNetworkMembership(userID, sib),
		adminNetworkMembership(astral.GenerateIdentity(), outsider),
	}}

	relayCases := []struct {
		name         string
		actor, forID *astral.Identity
		want         bool
	}{
		{"a sibling for the user", sib, userID, true},
		{"a stranger for the user", stranger, userID, false},
		{"a node in another user's swarm for the user", outsider, userID, false},
		{"a sibling for a stranger", sib, stranger, false},
	}

	for _, c := range relayCases {
		if got := mod.AuthorizeRelayFor(nil, relayForAction(c.actor, c.forID)); got != c.want {
			t.Errorf("AuthorizeRelayFor, %s: authorized %v, want %v", c.name, got, c.want)
		}
	}

	seeCases := []struct {
		name  string
		actor *astral.Identity
		want  bool
	}{
		{"a sibling", sib, true},
		{"a stranger", stranger, false},
	}

	for _, c := range seeCases {
		if got := mod.AuthorizeSeeSwarm(nil, &user.SeeSwarmAction{Action: auth.NewAction(c.actor)}); got != c.want {
			t.Errorf("AuthorizeSeeSwarm, %s: authorized %v, want %v", c.name, got, c.want)
		}
	}
}
