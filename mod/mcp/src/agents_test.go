package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

// testAgent is an agent as messaging mints one.
func testAgent() *mcp.Agent {
	return &mcp.Agent{
		Identity:  astral.GenerateIdentity(),
		Alias:     "scout",
		Token:     astral.String8(astral.GenerateIdentity().String()),
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
}

// The agent row records what messaging minted, so list_agents can answer the
// token later.
func TestStoreAgentRecordsTheCredential(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := testAgent()

	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	row, err := mod.db.FindAgent(agent.Identity)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	if row.Token != string(agent.Token) || row.Alias != string(agent.Alias) {
		t.Fatalf("stored %+v, want the minted credential", row)
	}
	if len(msg.deleted) != 0 {
		t.Fatal("a stored agent's participant was deleted")
	}
}

// A row that cannot be written takes the participant with it: a participant
// no agent row names holds a credential no mcp op can reach.
func TestAFailedAgentRowDeletesTheParticipant(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := testAgent()

	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	// the same identity again violates the row's unique index
	if err := mod.storeAgent(mod.ctx, agent); err == nil {
		t.Fatal("a second row for one identity was stored")
	}

	if len(msg.deleted) != 1 || !msg.deleted[0].IsEqual(agent.Identity) {
		t.Fatalf("deleted participants %v, want the one whose row failed", msg.deleted)
	}
}

// An agent whose participant is already gone is still deleted: the row left
// behind must stay removable.
func TestDeletingAnAgentWhoseParticipantIsGone(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := testAgent()
	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	row, err := mod.db.FindAgent(agent.Identity)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}

	msg.deleteErr = messagingmod.ErrIdentityNotFound

	if err = mod.deleteAgent(mod.ctx, row); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if _, err = mod.db.FindAgent(agent.Identity); err == nil {
		t.Fatal("agent row still present after delete")
	}
}

// Any other failure deleting the participant keeps the row, so the operator can
// run mcp.delete_agent again.
func TestAFailedParticipantDeleteKeepsTheAgentRow(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := testAgent()
	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	row, err := mod.db.FindAgent(agent.Identity)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}

	msg.deleteErr = errors.New("store is down")

	if err = mod.deleteAgent(mod.ctx, row); err == nil {
		t.Fatal("the deletion answered no error on a failed participant delete")
	}
	if _, err = mod.db.FindAgent(agent.Identity); err != nil {
		t.Fatalf("find agent after the failed delete: %v", err)
	}
}

// A row mcp.list_agents dropped between the two deletes is not an error: the
// agent is gone either way.
func TestDeletingAnAgentWhoseRowIsAlreadyGone(t *testing.T) {
	mod, _ := testAgentModule(t)
	agent := testAgent()
	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	row, err := mod.db.FindAgent(agent.Identity)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	if err = mod.db.DeleteAgent(agent.Identity); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	if err = mod.deleteAgent(mod.ctx, row); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
}

// A lookup that fails is not a withdrawal: the reads answer the error and no
// row is dropped.
func TestAFailedParticipantLookupDropsNoRow(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := testAgent()
	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	msg.findErr = errors.New("store is down")

	if _, err := mod.findAgent(agent.Identity); err == nil {
		t.Fatal("findAgent answered no error on a failed lookup")
	}
	if _, err := mod.Agents(); err == nil {
		t.Fatal("Agents answered no error on a failed lookup")
	}
	if _, err := mod.db.FindAgent(agent.Identity); err != nil {
		t.Fatalf("the row after a failed lookup: %v", err)
	}
}
