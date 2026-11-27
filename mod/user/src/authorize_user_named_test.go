package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// userNamedRule is one handler this module adds for an action that had none,
// asked about an actor.
type userNamedRule struct {
	name  string
	allow func(mod *Module, actor *astral.Identity) bool
}

func userNamedRules() []userNamedRule {
	other := astral.GenerateIdentity()
	return []userNamedRule{
		{"send", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeSend(nil, &messaging.SendAction{Action: auth.NewAction(actor), ToID: other})
		}},
		{"receive", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeReceive(nil, &messaging.ReceiveAction{Action: auth.NewAction(actor), FromID: other})
		}},
		{"read_mailbox", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeReadMailbox(nil, &messaging.ReadMailboxAction{Action: auth.NewAction(actor), MailboxID: other})
		}},
		{"register_signer", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeRegisterSigner(nil, &auth.RegisterSignerAction{Action: auth.NewAction(actor)})
		}},
		{"serve_objects describer", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleDescriber))
		}},
		{"serve_objects finder", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleFinder))
		}},
		{"serve_objects searcher", func(m *Module, actor *astral.Identity) bool {
			return m.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleSearcher))
		}},
	}
}

// TestUserNamedRulesAllowTheUser: each newly named action allows the user,
// and refuses a claimed node's own identity, a stranger and a zero actor.
func TestUserNamedRulesAllowTheUser(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: nodeID}

	for _, r := range userNamedRules() {
		if !r.allow(mod, userID) {
			t.Fatalf("%s: the user must be allowed", r.name)
		}
		for name, actor := range map[string]*astral.Identity{
			"this node": nodeID,
			"stranger":  astral.GenerateIdentity(),
			"zero":      {},
			"nil":       nil,
		} {
			if r.allow(mod, actor) {
				t.Fatalf("%s: %s must be refused", r.name, name)
			}
		}
	}
}

// TestUserNamedRulesAllowTheUnclaimedNode: an unclaimed node acts as the user
// for each newly named action, and a stranger is refused.
func TestUserNamedRulesAllowTheUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	for _, r := range userNamedRules() {
		if !r.allow(mod, nodeID) {
			t.Fatalf("%s: the unclaimed node acts as the user and must be allowed", r.name)
		}
		if r.allow(mod, astral.GenerateIdentity()) {
			t.Fatalf("%s: a stranger must be refused", r.name)
		}
	}
}
