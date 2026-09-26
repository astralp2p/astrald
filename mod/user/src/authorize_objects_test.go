package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// identityNode is an astral.Node that only answers Identity. Every other method
// panics on the embedded nil interface, so a handler that reaches past the
// identity check fails loudly rather than silently.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// TestAuthorizeObjectAccessGrantsTheNodeOnUnclaimedNode is the property the
// user-provisioning ceremony depends on: before any user or swarm exists, the
// node stores the derived user key and reads it back as itself. A local caller
// carrying no identity is promoted to the node (core/router.go), so the node's
// own identity is the actor these two handlers must grant — the user branch (nil
// here) cannot match.
func TestAuthorizeObjectAccessGrantsTheNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeStoreObjects(nil, &auth.StoreObjectsAction{Action: auth.NewAction(nodeID)}) {
		t.Fatal("the node must store objects on an unclaimed node; provisioning stores the user key before a user exists")
	}

	if !mod.AuthorizeSeeObjects(nil, &auth.SeeObjectsAction{Action: auth.NewAction(nodeID)}) {
		t.Fatal("the node must read back its own objects on an unclaimed node")
	}

	stranger := astral.GenerateIdentity()

	if mod.AuthorizeStoreObjects(nil, &auth.StoreObjectsAction{Action: auth.NewAction(stranger)}) {
		t.Fatal("a caller that is neither the node nor the user must not store")
	}

	if mod.AuthorizeSeeObjects(nil, &auth.SeeObjectsAction{Action: auth.NewAction(stranger)}) {
		t.Fatal("a caller that is neither the node nor the user must not read")
	}
}

// TestSeeObjectsGrantsUserAndNodeOnly pins the default holders of SeeObjects on a
// claimed node: the user identity and this node. A current swarm member is refused.
func TestSeeObjectsGrantsUserAndNodeOnly(t *testing.T) {
	mod, f := swarmFixture(t)

	for _, c := range f.cases() {
		a := &auth.SeeObjectsAction{Action: auth.NewAction(c.actor)}
		if got := mod.AuthorizeSeeObjects(nil, a); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}

// TestStoreObjectsGrantsUserAndNodeOnly pins the default holders of StoreObjects
// on a claimed node: the user identity and this node. A current swarm member is
// refused.
func TestStoreObjectsGrantsUserAndNodeOnly(t *testing.T) {
	mod, f := swarmFixture(t)

	for _, c := range f.cases() {
		a := &auth.StoreObjectsAction{Action: auth.NewAction(c.actor)}
		if got := mod.AuthorizeStoreObjects(nil, a); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}

// TestAuthorizeObjectAccessRefusesZeroActorOnUnclaimedNode pins the unclaimed-node
// case: the user identity is nil before a claim, and a nil or zero actor must not
// match it in any of the three object handlers.
func TestAuthorizeObjectAccessRefusesZeroActorOnUnclaimedNode(t *testing.T) {
	mod := &Module{node: &identityNode{id: astral.GenerateIdentity()}}

	for name, actor := range map[string]*astral.Identity{
		"nil":  nil,
		"zero": {},
	} {
		if mod.AuthorizeSeeObjects(nil, &auth.SeeObjectsAction{Action: auth.NewAction(actor)}) {
			t.Errorf("a %s actor reads objects on an unclaimed node", name)
		}

		if mod.AuthorizeStoreObjects(nil, &auth.StoreObjectsAction{Action: auth.NewAction(actor)}) {
			t.Errorf("a %s actor stores objects on an unclaimed node", name)
		}

		if mod.AuthorizeAdminObjects(nil, &auth.AdminObjectsAction{Action: auth.NewAction(actor)}) {
			t.Errorf("a %s actor administers objects on an unclaimed node", name)
		}
	}
}
