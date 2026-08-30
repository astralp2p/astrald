package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	dirmod "github.com/astralp2p/astrald/mod/dir"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type stubApphost struct {
	apphostmod.Module
	tokens  map[string]*astral.Identity
	deleted []string
}

func (s *stubApphost) DeleteAccessToken(token string) error {
	s.deleted = append(s.deleted, token)
	return nil
}

func (s *stubApphost) AuthenticateToken(token string) (*astral.Identity, error) {
	if id, ok := s.tokens[token]; ok {
		return id, nil
	}
	return nil, errors.New("invalid token")
}

type stubDir struct {
	dirmod.Module
	aliases map[string]*astral.Identity
}

func (s *stubDir) SetAlias(id *astral.Identity, alias string) error {
	if alias == "" {
		for k, v := range s.aliases {
			if v.IsEqual(id) {
				delete(s.aliases, k)
			}
		}
		return nil
	}
	s.aliases[alias] = id
	return nil
}

// why the raw form is tried first: the real directory parses an identity
// before it looks in the alias table (mod/dir/src/module.go), so a stub that
// only knew aliases would pass a caller the node would refuse — and fail one
// the node would serve.
func (s *stubDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, err := astral.ParseIdentity(name); err == nil {
		return id, nil
	}
	if id, ok := s.aliases[name]; ok {
		return id, nil
	}
	return nil, errors.New("not found")
}

func (s *stubDir) GetAlias(id *astral.Identity) (string, error) {
	for alias, aid := range s.aliases {
		if aid.IsEqual(id) {
			return alias, nil
		}
	}
	return "", errors.New("not found")
}

func (s *stubDir) DisplayName(id *astral.Identity) string {
	alias, _ := s.GetAlias(id)
	return alias
}

func testAgentModule(t *testing.T) (*Module, *stubApphost, *stubDir) {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbAgent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	apphost := &stubApphost{}
	dir := &stubDir{aliases: map[string]*astral.Identity{}}

	mod := &Module{db: db, config: defaultConfig}
	mod.Apphost = apphost
	mod.Dir = dir

	return mod, apphost, dir
}

func TestAssignAliasExplicit(t *testing.T) {
	mod, _, dir := testAgentModule(t)
	agentID := astral.GenerateIdentity()

	alias, err := mod.assignAlias(agentID, "my-agent")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if alias != "my-agent" {
		t.Fatalf("assigned %v, want my-agent", alias)
	}
	if !dir.aliases["my-agent"].IsEqual(agentID) {
		t.Fatal("alias not bound to agent")
	}
}

func TestAssignAliasTaken(t *testing.T) {
	mod, _, dir := testAgentModule(t)
	dir.aliases["my-agent"] = astral.GenerateIdentity()

	if _, err := mod.assignAlias(astral.GenerateIdentity(), "my-agent"); err == nil {
		t.Fatal("assign succeeded on a taken alias")
	}
}

// An empty alias binds nothing. The node holds many tenants' agents and the
// alias namespace is one, so a name it invents is a name a tenant may want.
func TestAssignAliasEmptyBindsNothing(t *testing.T) {
	mod, _, dir := testAgentModule(t)
	agentID := astral.GenerateIdentity()

	alias, err := mod.assignAlias(agentID, "")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if alias != "" {
		t.Fatalf("assigned %v, want no alias", alias)
	}
	if len(dir.aliases) != 0 {
		t.Fatalf("bound %v, want nothing", dir.aliases)
	}
}

func TestDeleteAgent(t *testing.T) {
	mod, apphost, dir := testAgentModule(t)
	agentID := astral.GenerateIdentity()

	err := mod.db.CreateAgent(&dbAgent{
		Identity:  agentID,
		Alias:     "my-agent",
		Token:     "token123",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	dir.aliases["my-agent"] = agentID

	row, err := mod.db.FindAgent(agentID)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}

	if err = mod.deleteAgent(row); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	if len(apphost.deleted) != 1 || apphost.deleted[0] != "token123" {
		t.Fatalf("revoked tokens %v, want [token123]", apphost.deleted)
	}
	if _, ok := dir.aliases["my-agent"]; ok {
		t.Fatal("alias still set after delete")
	}
	if _, err = mod.db.FindAgent(agentID); err == nil {
		t.Fatal("agent row still present after delete")
	}
}

func TestDBAgentRoundTrip(t *testing.T) {
	mod, _, _ := testAgentModule(t)
	agentID := astral.GenerateIdentity()

	err := mod.db.CreateAgent(&dbAgent{
		Identity:  agentID,
		Alias:     "a1",
		Token:     "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := mod.db.ListAgents()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %v rows", err, len(list))
	}

	if err = mod.db.DeleteAgent(agentID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err = mod.db.DeleteAgent(agentID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete: got %v, want gorm.ErrRecordNotFound", err)
	}
}
