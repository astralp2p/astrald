package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// stubObjects stores every object in nowhere and says it did.
type stubObjects struct {
	objectsmod.Module
	stored []astral.Object
}

func (s *stubObjects) WriteDefault() objectsmod.Repository { return nil }

func (s *stubObjects) Store(_ *astral.Context, _ objectsmod.Repository, obj astral.Object) (*astral.ObjectID, error) {
	s.stored = append(s.stored, obj)
	return &astral.ObjectID{}, nil
}

// CreateAccessToken issues a token for the identity and records it, or answers
// createErr when the test set one.
func (s *stubApphost) CreateAccessToken(id *astral.Identity, d astral.Duration) (*apphost.AccessToken, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}

	token := astral.GenerateIdentity().String()
	s.tokens[token] = id
	return &apphost.AccessToken{
		Identity:  id,
		Token:     astral.String8(token),
		ExpiresAt: astral.Time(time.Now().Add(time.Duration(d))),
	}, nil
}

// A participant create_identity mints has a mailbox the moment it is answered:
// the node hosts it under the hosting contract the identity signed, a delivery
// to it lands and it reads the message, with nothing else on the node knowing
// it exists.
func TestACreatedIdentityReceivesMail(t *testing.T) {
	mod, apphost, dir := testIdentityModule(t)

	cred, err := mod.CreateIdentity(mod.ctx, "scout", 0)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}

	if !mod.hosts(cred.Identity) {
		t.Fatal("the node does not host the created identity's mailbox")
	}
	if !dir.aliases["scout"].IsEqual(cred.Identity) || cred.Alias != "scout" {
		t.Fatalf("alias %q not bound to the created identity", cred.Alias)
	}
	if !apphost.tokens[string(cred.Token)].IsEqual(cred.Identity) {
		t.Fatal("the answered token is not one apphost issued for the identity")
	}

	obj := deliverOverRouter(t, mod, cred.Identity, &messaging.Message{ID: testID(1), Content: "welcome"})
	if _, ok := obj.(*astral.Ack); !ok {
		t.Fatalf("delivery answered %T, want an ack", obj)
	}

	res, err := mod.ReadMessages(context.Background(), cred.Identity, &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: testID(1)}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Content == nil || *res.Messages[0].Content != "welcome" {
		t.Fatalf("the created participant read %+v", res)
	}
}

// create_identity provisions two separate contracts from the new identity to
// this node: the relay contract, unchanged, and a hosting contract carrying one
// non-delegable host_mailbox_action permit that expires after
// Config.HostingDuration. Both are signed, indexed and stored, and the index
// row names the hosting one.
func TestCreateIdentityProvisionsASeparateHostingContract(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	mod.config.HostingDuration = 48 * time.Hour
	before := time.Now()

	cred, err := mod.CreateIdentity(mod.ctx, "", 0)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}

	hosting := hostingContracts(t, mod, cred.Identity)
	if len(hosting) != 1 {
		t.Fatalf("%v hosting contracts indexed, want 1", len(hosting))
	}
	if !hosting[0].Issuer.IsEqual(cred.Identity) {
		t.Fatalf("the hosting contract is issued by %v, want the new identity %v", hosting[0].Issuer, cred.Identity)
	}
	checkHostingContract(t, mod, hosting[0], before)

	relay, err := authorityOf(mod).SignedContracts().
		WithIssuer(cred.Identity).
		WithAction(&nodes.RelayForAction{}).
		Find(mod.ctx)
	if err != nil || len(relay) != 1 {
		t.Fatalf("relay contracts: %v, err %v; want 1", len(relay), err)
	}
	if len(relay[0].HasPermit(messaging.HostMailboxAction{}.ObjectType())) != 0 {
		t.Fatal("the relay contract carries the hosting permit; the two must be separate")
	}

	row, err := mod.db.FindMailbox(cred.Identity)
	if err != nil {
		t.Fatalf("index row: %v", err)
	}
	hostingID, _ := astral.ResolveObjectID(hosting[0])
	if row.ContractID == nil || !row.ContractID.IsEqual(hostingID) {
		t.Fatalf("the index row names %v, want the hosting contract %v", row.ContractID, hostingID)
	}

	if n := len(mod.Objects.(*stubObjects).stored); n != 3 {
		t.Fatalf("%v objects stored, want the key and both contracts", n)
	}
}

// checkHostingContract asserts the hosting contract's shape: subject this node,
// one permit for host_mailbox_action with no constraint and no delegation,
// expiring Config.HostingDuration after before.
func checkHostingContract(t *testing.T, mod *Module, sc *auth.SignedContract, before time.Time) {
	t.Helper()

	if !sc.Subject.IsEqual(mod.node.Identity()) {
		t.Fatalf("the contract's subject is %v, want this node %v", sc.Subject, mod.node.Identity())
	}
	if len(sc.Permits) != 1 {
		t.Fatalf("%v permits, want exactly the hosting one", len(sc.Permits))
	}
	p := sc.Permits[0]
	if string(p.Action) != (messaging.HostMailboxAction{}).ObjectType() || p.Delegation != 0 || p.Constraints != nil {
		t.Fatalf("permit %v delegation %v constraints %v, want host_mailbox_action, 0, none", p.Action, p.Delegation, p.Constraints)
	}

	expires := sc.ExpiresAt.Time()
	low, high := before.Add(mod.config.HostingDuration), time.Now().Add(mod.config.HostingDuration)
	if expires.Before(low.Add(-time.Second)) || expires.After(high.Add(time.Second)) {
		t.Fatalf("the contract expires at %v, want within %v..%v", expires, low, high)
	}
}

// A create that fails after the hosting contract is signed leaves no mailbox
// this node serves: the index row is written last, and the signed contract
// alone serves nothing.
func TestAFailedCreateLeavesNoServedMailbox(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	apphost.createErr = errors.New("token store is down")

	if _, err := mod.CreateIdentity(mod.ctx, "", 0); err == nil {
		t.Fatal("the create answered no error when the token could not be issued")
	}

	rows, err := mod.db.ListMailboxes()
	if err != nil || len(rows) != 0 {
		t.Fatalf("index rows after a failed create: %v, err %v; want none", len(rows), err)
	}
	if n := mod.mailboxes.Len(); n != 0 {
		t.Fatalf("%v mailboxes mirrored after a failed create, want none", n)
	}
}
