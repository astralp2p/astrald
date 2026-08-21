package mcp

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
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
func TestNewAgentIsNotVisible(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := createTestAgent(t, mod, "")

	row, err := mod.db.FindAgent(id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if row.Visible {
		t.Fatal("a new agent is visible")
	}
}

// create_agent takes the flag, so an agent is minted open in one write. The
// router reads the mirror, so the row alone is not reach.
func TestRegisterAgentVisible(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := astral.GenerateIdentity()

	err := mod.registerAgent(&dbAgent{
		Identity:  id,
		Token:     "token-open",
		ExpiresAt: time.Now().Add(time.Hour),
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	row, err := mod.db.FindAgent(id)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !row.Visible {
		t.Fatal("the row reads closed after a create that asked for open")
	}
	if !mod.visible.Contains(id.String()) {
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

	if mod.visible.Contains(id.String()) {
		t.Fatal("an agent minted without the flag is open to the router")
	}
	if !mod.agentIDs.Contains(id.String()) {
		t.Fatal("the agent is not registered")
	}
}

func TestSetVisibleRoundTrip(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	id := createTestAgent(t, mod, "")

	for _, want := range []bool{true, false, true} {
		if err := mod.db.SetVisible(id, want); err != nil {
			t.Fatalf("set %v: %v", want, err)
		}
		row, err := mod.db.FindAgent(id)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if row.Visible != want {
			t.Fatalf("visible reads %v, want %v", row.Visible, want)
		}
	}
}

func TestSetVisibleUnknownAgent(t *testing.T) {
	mod, _, _ := testAgentModule(t)

	if err := mod.db.SetVisible(astral.GenerateIdentity(), true); err == nil {
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
	typ := reflect.TypeOf(mcp.AgentInfo{})

	for i := range typ.NumField() {
		name := typ.Field(i).Name
		if strings.Contains(strings.ToLower(name), "token") {
			t.Fatalf("AgentInfo carries %v; a record read by non-owners must hold no credential", name)
		}
	}
}

// dbAgentBeforeRename is the agent row as it stood before the rename, so the
// migration runs against a table GORM created rather than hand-written DDL.
type dbAgentBeforeRename struct {
	Identity  *astral.Identity `gorm:"uniqueIndex"`
	Alias     string
	Token     string `gorm:"uniqueIndex"`
	ExpiresAt time.Time
	CreatedAt time.Time
	Exposed   bool
}

func (dbAgentBeforeRename) TableName() string {
	return dbAgent{}.TableName()
}

// A node upgraded across the rename keeps every agent's decision: the old
// `exposed` column is moved rather than left beside a fresh `visible` one,
// which would read every open agent as closed.
func TestMigrateAgentsCarriesTheOldColumn(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db := &DB{DB: gdb}

	if err := gdb.AutoMigrate(&dbAgentBeforeRename{}); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	for _, seed := range []struct {
		alias   string
		exposed bool
	}{{"open", true}, {"closed", false}} {
		row := &dbAgentBeforeRename{
			Identity:  astral.GenerateIdentity(),
			Alias:     seed.alias,
			Token:     "tok-" + seed.alias,
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
			Exposed:   seed.exposed,
		}
		if err := gdb.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", seed.alias, err)
		}
	}

	if err := db.MigrateAgents(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if db.Migrator().HasColumn(&dbAgent{}, "exposed") {
		t.Fatal("the old column survived the rename")
	}

	rows, err := db.ListAgents()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: want 2, got %d", len(rows))
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Alias] = r.Visible
	}
	if !got["open"] {
		t.Error("the open agent came back closed")
	}
	if got["closed"] {
		t.Error("the closed agent came back open")
	}
}

// A fresh node has no `exposed` column and the migration is the plain schema
// creation, run twice to prove it is idempotent.
func TestMigrateAgentsOnAFreshDatabase(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db := &DB{DB: gdb}

	for i := 0; i < 2; i++ {
		if err := db.MigrateAgents(); err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
	}
	if !db.Migrator().HasColumn(&dbAgent{}, "visible") {
		t.Fatal("visible column missing")
	}
}
