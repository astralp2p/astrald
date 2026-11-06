package sys

import (
	"testing"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

func TestNodeState(t *testing.T) {
	ctx := astral.NewContext(nil)

	fresh, _, _ := freshModule()
	if got := fresh.NodeState(ctx); got != user.StateFresh {
		t.Errorf("fresh: NodeState = %v; want fresh", got)
	}

	appointed, _, _ := freshModule()
	appointed.appointment.Manager.Set(ctx, astral.GenerateIdentity())
	if got := appointed.NodeState(ctx); got != user.StateUnclaimed {
		t.Errorf("appointed: NodeState = %v; want unclaimed", got)
	}

	if got := (&Module{}).NodeState(ctx); got != user.StateFresh {
		t.Errorf("no user module, no manager: NodeState = %v; want fresh", got)
	}

	// an expired or expelled claim keeps the appointment closed
	owned := &Module{}
	owned.User = &stubUser{ready: closedChan(), owned: true}
	if got := owned.NodeState(ctx); got != user.StateUnclaimed {
		t.Errorf("owned before, no manager: NodeState = %v; want unclaimed", got)
	}

	// a claim keeps the manager and reads claimed
	claimed := &Module{}
	claimed.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}
	claimed.appointment.Manager.Set(ctx, astral.GenerateIdentity())
	if got := claimed.NodeState(ctx); got != user.StateClaimed {
		t.Errorf("claimed with manager: NodeState = %v; want claimed", got)
	}

	// user module not ready, ctx cancelled: fresh admits the fewest ops
	notReady := &Module{}
	notReady.User = &stubUser{id: astral.GenerateIdentity(), ready: make(chan struct{})}
	cctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()
	if got := notReady.NodeState(cctx); got != user.StateFresh {
		t.Errorf("not ready + ctx cancelled: NodeState = %v; want fresh", got)
	}
}

// TestManagerReadsTheLegacyRecord: a node apphost appointed keeps its manager
// and stays unclaimed.
func TestManagerReadsTheLegacyRecord(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod, _, _ := freshModule()
	old := astral.GenerateIdentity()
	mod.legacy.Manager.Set(ctx, old)

	if !mod.Manager().IsEqual(old) {
		t.Fatalf("Manager = %v; want the legacy record %v", mod.Manager(), old)
	}
	if got := mod.NodeState(ctx); got != user.StateUnclaimed {
		t.Fatalf("NodeState = %v; want unclaimed", got)
	}

	current := astral.GenerateIdentity()
	mod.appointment.Manager.Set(ctx, current)
	if !mod.Manager().IsEqual(current) {
		t.Fatal("the legacy record outranks the sys record")
	}
}
