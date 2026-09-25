package mcp

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// undo runs through the same tool, so what the answer reports is whether this
// call moved the message — messaging's answer, carried as changed, for the
// authenticated agent's own row.
func TestArchiveReportsWhetherThisCallMovedIt(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := astral.GenerateIdentity()
	id := messaging.NewMessageID()

	for _, c := range []struct {
		undo, changed bool
	}{{false, true}, {false, false}, {true, true}, {true, false}} {
		msg.changed = c.changed

		_, out, err := mod.archiveTool(agent)(context.Background(), nil, archiveIn{
			Box: messaging.BoxInbox, ID: id.String(), Undo: c.undo,
		})
		if err != nil {
			t.Fatalf("archive(undo=%v): %v", c.undo, err)
		}
		if out.Changed != c.changed {
			t.Fatalf("archive(undo=%v): changed %v, want messaging's %v", c.undo, out.Changed, c.changed)
		}
		if msg.archiveUndo != c.undo {
			t.Fatalf("archive(undo=%v) reached messaging as undo=%v", c.undo, msg.archiveUndo)
		}
		if msg.archiveRef.Box != messaging.BoxInbox || msg.archiveRef.ID != id {
			t.Fatalf("archive named %+v, want the inbox row %v", msg.archiveRef, id)
		}
	}

	msg.checkCalledAs(t, agent)
}

// A box that is neither direction is refused before messaging is asked.
func TestArchiveRefusesABoxThatIsNotOne(t *testing.T) {
	mod, msg := testAgentModule(t)

	_, _, err := mod.archiveTool(astral.GenerateIdentity())(context.Background(), nil, archiveIn{
		Box: "archive", ID: messaging.NewMessageID().String(),
	})
	if err == nil {
		t.Fatal("archive is a state, not a box, and must be refused")
	}
	if len(msg.callers) != 0 {
		t.Fatal("messaging was asked about a box that is not one")
	}
}
