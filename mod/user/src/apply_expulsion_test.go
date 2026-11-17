package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// cancelCheckingNodes records, at each CloseLinks call, whether the sibling's
// link-maintenance task was already cancelled.
type cancelCheckingNodes struct {
	nodesmod.Module
	cancelled        *bool
	cancelledAtClose []bool
}

func (n *cancelCheckingNodes) CloseLinks(*astral.Identity) error {
	n.cancelledAtClose = append(n.cancelledAtClose, *n.cancelled)
	return nil
}

// TestApplyExpulsionCancelsSiblingBeforeClosingLinks checks that a member applying
// an expulsion of a sibling cancels the sibling's link-maintenance task before it
// closes the links; a task still running at close sees a broken link and redials.
func TestApplyExpulsionCancelsSiblingBeforeClosingLinks(t *testing.T) {
	var cancelled bool
	nodes := &cancelCheckingNodes{cancelled: &cancelled}
	mod := &Module{node: &identityNode{id: astral.GenerateIdentity()}, log: log.New(nil)}
	mod.Nodes = nodes
	sib := astral.GenerateIdentity()
	mod.addSibling(sib, func() { cancelled = true }, func() {})

	mod.applyExpulsion(sib)

	if len(nodes.cancelledAtClose) != 1 {
		t.Fatalf("CloseLinks called %d times, want 1", len(nodes.cancelledAtClose))
	}
	if !nodes.cancelledAtClose[0] {
		t.Fatal("links closed while the sibling's link-maintenance task was still running")
	}
	if _, ok := mod.sibs.Get(sib.String()); ok {
		t.Fatal("the expelled sibling is still registered")
	}
}
