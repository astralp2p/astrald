package mcp

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

func createTestAgent(t *testing.T, mod *Module, alias string) *astral.Identity {
	t.Helper()
	id := astral.GenerateIdentity()
	err := mod.db.CreateAgent(&dbAgent{
		Identity:  id,
		Alias:     alias,
		Token:     "token-" + id.String()[:8],
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return id
}

// A fresh agent is closed. The column's zero value is the safe state, so a row
// written by anything that does not know about the flag is closed too.
func TestNewAgentIsNotExposed(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := createTestAgent(t, mod, "")

	row, err := mod.db.FindAgent(id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if row.Exposed {
		t.Fatal("a new agent is exposed")
	}
}

// create_agent takes the flag, so an agent is minted open in one write. The
// router reads the mirror, so the row alone is not reach.
func TestRegisterAgentExposed(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := astral.GenerateIdentity()

	err := mod.registerAgent(&dbAgent{
		Identity:  id,
		Token:     "token-open",
		ExpiresAt: time.Now().Add(time.Hour),
		Exposed:   true,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	row, err := mod.db.FindAgent(id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !row.Exposed {
		t.Fatal("the row reads closed after a create that asked for open")
	}
	if !mod.exposed.Contains(id.String()) {
		t.Fatal("the agent is open in the row and closed to the router")
	}
	if !mod.agentIDs.Contains(id.String()) {
		t.Fatal("the agent is not registered")
	}
}

// The flag omitted mints the agent every caller minted before this argument
// existed: registered, and reachable by nobody.
func TestRegisterAgentClosedByDefault(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := astral.GenerateIdentity()

	err := mod.registerAgent(&dbAgent{
		Identity:  id,
		Token:     "token-closed",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if mod.exposed.Contains(id.String()) {
		t.Fatal("an agent minted without the flag is open to the router")
	}
	if !mod.agentIDs.Contains(id.String()) {
		t.Fatal("the agent is not registered")
	}
}

func TestSetExposedRoundTrip(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := createTestAgent(t, mod, "")

	for _, want := range []bool{true, false, true} {
		if err := mod.db.SetExposed(id, want); err != nil {
			t.Fatalf("set %v: %v", want, err)
		}
		row, err := mod.db.FindAgent(id)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if row.Exposed != want {
			t.Fatalf("exposed reads %v, want %v", row.Exposed, want)
		}
	}
}

func TestSetExposedUnknownAgent(t *testing.T) {
	mod, _, _ := testAgentModule(t)

	if err := mod.db.SetExposed(astral.GenerateIdentity(), true); err == nil {
		t.Fatal("set succeeded on an agent that does not exist")
	}
}

// An agent minted without an alias is deleted like any other. deleteAgent used
// to unset an alias unconditionally, which is a write with nothing to unset.
func TestDeleteAgentWithoutAlias(t *testing.T) {
	mod, apphost, _ := testAgentModule(t)
	id := createTestAgent(t, mod, "")

	row, err := mod.db.FindAgent(id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if err = mod.deleteAgent(row); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(apphost.deleted) != 1 {
		t.Fatalf("revoked %v tokens, want 1", len(apphost.deleted))
	}
	if _, err = mod.db.FindAgent(id); err == nil {
		t.Fatal("the row survived deletion")
	}
}

// The record a caller reads about someone else's agent carries no secret. A
// field added later that does is what this guards against, not today's shape.
func TestAgentInfoCarriesNoToken(t *testing.T) {
	typ := reflect.TypeOf(mcpmod.AgentInfo{})

	for i := range typ.NumField() {
		name := typ.Field(i).Name
		if strings.Contains(strings.ToLower(name), "token") {
			t.Fatalf("AgentInfo carries %v; a record read by non-owners must hold no credential", name)
		}
	}
}
