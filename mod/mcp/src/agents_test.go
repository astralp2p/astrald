package mcp

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
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
	grants  map[string][]*auth.Permit // node-local grants, keyed by identity

	// revokeErr is what Revoke answers instead of withdrawing a grant.
	revokeErr error
}

func (s *stubApphost) DeleteAccessToken(token string) error {
	s.deleted = append(s.deleted, token)
	return nil
}

// Grants answers what apphost.Module.Grants answers: every permit the node
// holds for the identity, expired ones included.
func (s *stubApphost) Grants(id *astral.Identity) ([]*auth.Permit, error) {
	return s.grants[id.String()], nil
}

func (s *stubApphost) Revoke(id *astral.Identity, action string) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}

	list := s.grants[id.String()]

	for i, permit := range list {
		if string(permit.Action) == action {
			s.grants[id.String()] = append(list[:i:i], list[i+1:]...)
			return nil
		}
	}

	return gorm.ErrRecordNotFound
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
	// why the whole schema and not the agent table alone: deleting an agent
	// deletes the mail it owns, so a store holding one table and not the other
	// is a shape no node ever runs.
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	apphost := &stubApphost{grants: map[string][]*auth.Permit{}}
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

// A deleted agent's identity holds no grant afterwards, and no other identity
// loses one. The first half is what an external provider reads on every call:
// ServeObjects is checked against the identity, not against an agent row, so a
// grant the deletion left behind keeps the deleted agent serving objects.
func TestDeletingAnAgentRevokesTheGrantsItsIdentityHolds(t *testing.T) {
	mod, apphost, _ := testAgentModule(t)
	agentID, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	// the second permit is the one apphost.register writes, so the deletion is
	// measured against a grant the agent did not ask mcp for
	apphost.grants[agentID.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
		{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())},
	}
	apphost.grants[other.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}

	if err := mod.deleteAgent(mustCreateAgent(t, mod, agentID)); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	grants, err := apphost.Grants(agentID)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("the deleted agent still holds %v grants, want none", len(grants))
	}

	grants, err = apphost.Grants(other)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("another identity holds %v grants after the delete, want 1", len(grants))
	}
}

// A grant the node no longer holds is not an error the deletion reports: the
// listing and the revoke are two statements, and a revoke that lands between
// them leaves the state the deletion wanted.
func TestDeletingAnAgentToleratesAGrantAlreadyRevoked(t *testing.T) {
	mod, apphost, _ := testAgentModule(t)
	row := mustCreateAgent(t, mod, astral.GenerateIdentity())

	apphost.grants[row.Identity.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}
	apphost.revokeErr = gorm.ErrRecordNotFound

	if err := mod.deleteAgent(row); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	if _, err := mod.db.FindAgent(row.Identity); err == nil {
		t.Fatal("agent row still present after delete")
	}
}

// A revoke that fails for any other reason stops the deletion and keeps the
// row, so the operator can run mcp.delete_agent again. A run that removed the
// row would leave the grant with no record naming who holds it.
func TestAFailedRevokeKeepsTheAgentRow(t *testing.T) {
	mod, apphost, _ := testAgentModule(t)
	row := mustCreateAgent(t, mod, astral.GenerateIdentity())

	apphost.grants[row.Identity.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}
	apphost.revokeErr = errors.New("store is down")

	if err := mod.deleteAgent(row); err == nil {
		t.Fatal("the deletion answered no error on a failed revoke")
	}

	if _, err := mod.db.FindAgent(row.Identity); err != nil {
		t.Fatalf("find agent after the failed delete: %v", err)
	}
}

// mustCreateAgent stores an agent for identity and answers the row deleteAgent
// takes.
func mustCreateAgent(t *testing.T, mod *Module, identity *astral.Identity) *dbAgent {
	t.Helper()

	err := mod.db.CreateAgent(&dbAgent{
		Identity:  identity,
		Token:     "token123",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	row, err := mod.db.FindAgent(identity)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}

	return row
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
