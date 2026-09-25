package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// An agent row is created, listed and deleted once. A second delete answers
// gorm.ErrRecordNotFound, which is how an agent already gone reads.
func TestDBAgentRoundTrip(t *testing.T) {
	db := testDB(t)
	agentID := astral.GenerateIdentity()

	err := db.CreateAgent(&dbAgent{
		Identity:  agentID,
		Alias:     "a1",
		Token:     "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := db.ListAgents()
	if err != nil || len(list) != 1 || !list[0].Identity.IsEqual(agentID) {
		t.Fatalf("list: %v, %v rows, want the one created", err, len(list))
	}

	if err = db.DeleteAgent(agentID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err = db.DeleteAgent(agentID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete: got %v, want gorm.ErrRecordNotFound", err)
	}

	if list, err = db.ListAgents(); err != nil || len(list) != 0 {
		t.Fatalf("list after delete: %v, %v rows, want none", err, len(list))
	}
}
