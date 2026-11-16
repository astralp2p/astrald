package sys

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

func configureNodeState(actor *astral.Identity) *auth.ConfigureNodeStateAction {
	return &auth.ConfigureNodeStateAction{Action: auth.NewAction(actor)}
}

// TestManagerHoldsConfigureNodeState: the manager holds the action in every
// state, a claimed node included; nobody else holds it through sys.
func TestManagerHoldsConfigureNodeState(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod, _, u := freshModule()
	manager := astral.GenerateIdentity()

	if mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("a node without a manager granted the action")
	}

	mod.appointment.Manager.Set(ctx, manager)
	if !mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("the manager does not hold the action")
	}

	u.id = astral.GenerateIdentity() // claimed
	if !mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("the manager lost the action after the claim")
	}
	if mod.AuthorizeConfigureNodeState(ctx, configureNodeState(astral.GenerateIdentity())) {
		t.Fatal("another identity holds the action")
	}
}

func vfsManage(actor *astral.Identity) *vfsapi.ManageAction {
	return &vfsapi.ManageAction{Action: auth.NewAction(actor)}
}

func vfsRead(actor, vfsID *astral.Identity) *vfsapi.ReadAction {
	return &vfsapi.ReadAction{Action: auth.NewAction(actor), VFSID: vfsID}
}

// TestManagerHoldsVFSManageAndRead: the manager holds the vfs manage action
// and the vfs read action for any VFS, in every state; a node without a
// manager grants neither, and nobody else, the zero identity included, holds
// either through sys. A new appointment moves both to the new manager.
func TestManagerHoldsVFSManageAndRead(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod, _, u := freshModule()
	manager, other, vfsID := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	for _, actor := range []*astral.Identity{manager, other, astral.Anyone} {
		if mod.AuthorizeVFSManage(ctx, vfsManage(actor)) || mod.AuthorizeVFSRead(ctx, vfsRead(actor, vfsID)) {
			t.Fatalf("a node without a manager granted %v a vfs action", actor)
		}
	}

	mod.appointment.Manager.Set(ctx, manager)
	for name, vfs := range map[string]*astral.Identity{"another VFS": vfsID, "the manager's VFS": manager, "the zero VFS": astral.Anyone} {
		if !mod.AuthorizeVFSRead(ctx, vfsRead(manager, vfs)) {
			t.Fatalf("the manager does not hold the read action for %v", name)
		}
	}
	if !mod.AuthorizeVFSManage(ctx, vfsManage(manager)) {
		t.Fatal("the manager does not hold the manage action")
	}

	u.id = astral.GenerateIdentity() // claimed
	if !mod.AuthorizeVFSManage(ctx, vfsManage(manager)) || !mod.AuthorizeVFSRead(ctx, vfsRead(manager, vfsID)) {
		t.Fatal("the manager lost a vfs action after the claim")
	}
	for _, actor := range []*astral.Identity{other, u.id, astral.Anyone} {
		if mod.AuthorizeVFSManage(ctx, vfsManage(actor)) || mod.AuthorizeVFSRead(ctx, vfsRead(actor, vfsID)) {
			t.Fatalf("%v holds a vfs action through sys", actor)
		}
	}

	successor := astral.GenerateIdentity()
	mod.appointment.Manager.Set(ctx, successor)
	if mod.AuthorizeVFSManage(ctx, vfsManage(manager)) || mod.AuthorizeVFSRead(ctx, vfsRead(manager, vfsID)) {
		t.Fatal("the former manager keeps a vfs action")
	}
	if !mod.AuthorizeVFSManage(ctx, vfsManage(successor)) || !mod.AuthorizeVFSRead(ctx, vfsRead(successor, vfsID)) {
		t.Fatal("the new manager does not hold the vfs actions")
	}
}
