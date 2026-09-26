package messaging

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	dirmod "github.com/astralp2p/astrald/mod/dir"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
	"gorm.io/gorm"
)

type stubApphost struct {
	apphostmod.Module
	tokens map[string]*astral.Identity // issued tokens, keyed by token
	grants map[string][]*auth.Permit   // node-local grants, keyed by identity

	// revokeErr is what Revoke answers instead of withdrawing a grant.
	revokeErr error

	// createErr is what CreateAccessToken answers instead of issuing a token.
	createErr error

	// deleteTokensErr is what DeleteAccessTokens answers instead of revoking.
	deleteTokensErr error
}

// DeleteAccessTokens answers what apphost.Module.DeleteAccessTokens answers:
// every token of the identity goes, and an identity holding none is no error.
func (s *stubApphost) DeleteAccessTokens(id *astral.Identity) error {
	if s.deleteTokensErr != nil {
		return s.deleteTokensErr
	}

	for token, holder := range s.tokens {
		if holder.IsEqual(id) {
			delete(s.tokens, token)
		}
	}
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

// testIdentityModule is the messaging module over a store holding the whole
// schema, with apphost and the directory stubbed.
//
// why the whole schema and not the hosting index alone: deleting a participant
// deletes the mail it owns, so a store holding one table and not the other is a
// shape no node ever runs.
func testIdentityModule(t *testing.T) (*Module, *stubApphost, *stubDir) {
	t.Helper()

	mod := testMessagingModule(t)

	apphost := &stubApphost{
		tokens: map[string]*astral.Identity{},
		grants: map[string][]*auth.Permit{},
	}
	dir := &stubDir{aliases: map[string]*astral.Identity{}}

	mod.Apphost = apphost
	mod.Dir = dir

	return mod, apphost, dir
}

func TestAssignAliasExplicit(t *testing.T) {
	mod, _, dir := testIdentityModule(t)
	identity := astral.GenerateIdentity()

	alias, err := mod.assignAlias(identity, "my-agent")
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if alias != "my-agent" {
		t.Fatalf("assigned %v, want my-agent", alias)
	}
	if !dir.aliases["my-agent"].IsEqual(identity) {
		t.Fatal("alias not bound to the participant")
	}
}

func TestAssignAliasTaken(t *testing.T) {
	mod, _, dir := testIdentityModule(t)
	dir.aliases["my-agent"] = astral.GenerateIdentity()

	if _, err := mod.assignAlias(astral.GenerateIdentity(), "my-agent"); err == nil {
		t.Fatal("assign succeeded on a taken alias")
	}
}

// An empty alias binds nothing. The node holds many tenants' participants and
// the alias namespace is one, so a name it invents is a name a tenant may want.
func TestAssignAliasEmptyBindsNothing(t *testing.T) {
	mod, _, dir := testIdentityModule(t)
	identity := astral.GenerateIdentity()

	alias, err := mod.assignAlias(identity, "")
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

// Every token the identity holds is revoked, not only the one create_identity
// issued, and no other identity's token is. The mailbox is withdrawn from this
// node and the hosting contract is not revoked: it stays indexed and valid
// until it expires.
func TestDeleteIdentity(t *testing.T) {
	mod, apphost, dir := testIdentityModule(t)
	identity := hostedParticipant(t, mod)
	other := astral.GenerateIdentity()

	dir.aliases["my-agent"] = identity
	apphost.tokens["token123"] = identity
	apphost.tokens["reissued"] = identity
	apphost.tokens["kept"] = other

	if err := mod.deleteIdentity(identity); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	if len(apphost.tokens) != 1 || !apphost.tokens["kept"].IsEqual(other) {
		t.Fatalf("tokens left %v, want only the other identity's", apphost.tokens)
	}
	if _, ok := dir.aliases["my-agent"]; ok {
		t.Fatal("alias still set after delete")
	}
	if _, err := mod.db.FindMailbox(identity); err == nil {
		t.Fatal("index row still present after delete")
	}
	if mod.hosts(identity) {
		t.Fatal("the node still hosts the deleted identity's mailbox")
	}
	if n := len(hostingContracts(t, mod, identity)); n != 1 {
		t.Fatalf("%v hosting contracts stay indexed after the delete, want the one provisioned", n)
	}
}

// An identity that is not a participant is not found, and nothing is revoked
// on its behalf.
func TestDeletingAnUnknownIdentityIsNotFound(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	stranger := astral.GenerateIdentity()
	apphost.tokens["token123"] = stranger

	err := mod.DeleteIdentity(mod.ctx, stranger)
	if !errors.Is(err, messagingmod.ErrIdentityNotFound) {
		t.Fatalf("delete an unknown identity: got %v, want %v", err, messagingmod.ErrIdentityNotFound)
	}
	if len(apphost.tokens) != 1 {
		t.Fatal("deleting an unknown identity revoked its token")
	}
}

// FindIdentity names every identity the hosting index names, served or not: a
// row whose hosting contract expired is still a participant. A withdrawn
// participant, a stranger and the zero identity are not found.
func TestFindIdentityNamesTheIndexedParticipants(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	hosted := hostedParticipant(t, mod)
	expired := astral.GenerateIdentity()
	if err := mod.db.CreateMailbox(expired, &astral.ObjectID{Size: 1}, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	for name, id := range map[string]*astral.Identity{"hosted": hosted, "expired": expired} {
		if err := mod.FindIdentity(id); err != nil {
			t.Fatalf("find the %v participant: %v", name, err)
		}
	}

	if err := mod.DeleteIdentity(mod.ctx, hosted); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	for name, id := range map[string]*astral.Identity{
		"withdrawn": hosted,
		"stranger":  astral.GenerateIdentity(),
		"zero":      &astral.Identity{},
		"nil":       nil,
	} {
		if err := mod.FindIdentity(id); !errors.Is(err, messagingmod.ErrIdentityNotFound) {
			t.Fatalf("find the %v identity: got %v, want %v", name, err, messagingmod.ErrIdentityNotFound)
		}
	}
}

// A deleted participant's identity holds no grant afterwards, and no other
// identity loses one. The first half is what an external provider reads on
// every call: ServeObjects is checked against the identity, not against a
// participant row, so a grant the deletion left behind keeps the deleted
// participant serving objects.
func TestDeletingAnIdentityRevokesTheGrantsItHolds(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	identity := hostedParticipant(t, mod)
	other := astral.GenerateIdentity()

	// the second permit is the one apphost.register writes, so the deletion is
	// measured against a grant the participant did not ask messaging for
	apphost.grants[identity.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
		{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())},
	}
	apphost.grants[other.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}

	if err := mod.deleteIdentity(identity); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	grants, err := apphost.Grants(identity)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("the deleted participant still holds %v grants, want none", len(grants))
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
func TestDeletingAnIdentityToleratesAGrantAlreadyRevoked(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	identity := hostedParticipant(t, mod)

	apphost.grants[identity.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}
	apphost.revokeErr = gorm.ErrRecordNotFound

	if err := mod.deleteIdentity(identity); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	if _, err := mod.db.FindMailbox(identity); err == nil {
		t.Fatal("index row still present after delete")
	}
}

// A revoke that fails for any other reason stops the deletion and keeps the
// row, so the operator can run messaging.delete_identity again. A run that
// removed the row would leave the grant with no record naming who holds it.
func TestAFailedRevokeKeepsTheIndexRow(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	identity := hostedParticipant(t, mod)

	apphost.grants[identity.String()] = []*auth.Permit{
		{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())},
	}
	apphost.revokeErr = errors.New("store is down")

	if err := mod.deleteIdentity(identity); err == nil {
		t.Fatal("the deletion answered no error on a failed revoke")
	}

	if _, err := mod.db.FindMailbox(identity); err != nil {
		t.Fatalf("find the index row after the failed delete: %v", err)
	}
}

// A token revoke that fails stops the deletion before anything else changes:
// the tokens, the alias, the index row, the mail and the hosting all stay, so
// the operator can run messaging.delete_identity again.
func TestAFailedTokenRevokeKeepsTheMailbox(t *testing.T) {
	mod, apphost, dir := testIdentityModule(t)
	identity := hostedParticipant(t, mod)
	dir.aliases["scout"] = identity
	apphost.tokens["issued"] = identity
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: astral.GenerateIdentity(), Recipient: identity, Content: "x",
	})
	apphost.deleteTokensErr = errors.New("token store is down")

	if err := mod.DeleteIdentity(mod.ctx, identity); err == nil {
		t.Fatal("the deletion answered no error on a failed token revoke")
	}

	if len(apphost.tokens) != 1 || !dir.aliases["scout"].IsEqual(identity) {
		t.Fatal("a deletion that failed on its tokens changed the tokens or the alias")
	}
	if _, err := mod.db.FindMailbox(identity); err != nil || !mod.hosts(identity) {
		t.Fatalf("the mailbox is not kept after a failed token revoke: row err %v, hosted %v", err, mod.hosts(identity))
	}
	if n := countOwned(t, mod.db, identity); n != 1 {
		t.Fatalf("%v messages kept after a failed token revoke, want 1", n)
	}
}

func TestDBMailboxRoundTrip(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	identity := astral.GenerateIdentity()
	contractID := &astral.ObjectID{Size: 1}
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if err := mod.db.CreateMailbox(identity, contractID, expiresAt); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := mod.db.ListMailboxes()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v, %v rows", err, len(list))
	}
	if !list[0].ContractID.IsEqual(contractID) || !list[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("stored %v until %v, want %v until %v", list[0].ContractID, list[0].ExpiresAt, contractID, expiresAt)
	}

	if err = mod.db.DeleteMailbox(identity); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err = mod.db.DeleteMailbox(identity); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete: got %v, want gorm.ErrRecordNotFound", err)
	}
}
