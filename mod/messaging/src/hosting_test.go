package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

// The hosting authority, verified against the real auth module: its chain
// walk, its contract index, its signature checks and its expiry. Every mailbox
// in these tests is provisioned by the module's own code, and every contract is
// signed with a key the test holds.

// hostingPermit is the permit create_identity writes, with the constraints the
// test names.
func hostingPermit(constraints ...astral.Object) *auth.Permit {
	p := &auth.Permit{Action: astral.String8(messaging.HostMailboxAction{}.ObjectType())}
	if len(constraints) == 0 {
		return p
	}

	p.Constraints = astral.NewBundle()
	for _, c := range constraints {
		if err := p.Constraints.Append(c); err != nil {
			panic(err)
		}
	}
	return p
}

// newContract is an unsigned contract issuer→subject carrying permits until
// expiresAt.
func newContract(issuer, subject *astral.Identity, expiresAt time.Time, permits ...*auth.Permit) *auth.Contract {
	return &auth.Contract{
		Issuer:    issuer,
		Subject:   subject,
		Permits:   permits,
		ExpiresAt: astral.Time(expiresAt),
	}
}

// signContract signs the contract with the keys the module's keyring holds.
func signContract(t *testing.T, mod *Module, c *auth.Contract) *auth.SignedContract {
	t.Helper()

	sc := &auth.SignedContract{Contract: c}
	if err := authorityOf(mod).SignContract(mod.ctx, sc); err != nil {
		t.Fatalf("sign contract: %v", err)
	}
	return sc
}

// entryNaming is an index entry that names sc and claims the expiry the test
// gives — the shape an entry takes when the index and the authority disagree.
func entryNaming(t *testing.T, sc *auth.SignedContract, expiresAt time.Time) mailbox {
	t.Helper()

	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("contract id: %v", err)
	}
	return mailbox{ContractID: id, ExpiresAt: &expiresAt}
}

// mustRecord writes the identity's index row as entry says, and mirrors it.
func mustRecord(t *testing.T, mod *Module, identity *astral.Identity, entry mailbox) {
	t.Helper()

	if err := mod.recordMailbox(identity, entry); err != nil {
		t.Fatalf("record mailbox: %v", err)
	}
}

// askHosting asks the authority directly whether node may host mailbox's
// mailbox.
func askHosting(mod *Module, node, mailbox *astral.Identity) bool {
	return authorityOf(mod).Authorize(mod.ctx, &messaging.HostMailboxAction{
		Action:    auth.NewAction(node),
		MailboxID: mailbox,
	})
}

// The root rule allows an identity to host its own mailbox and nothing else,
// and never the anonymous one.
func TestTheHostingRootAllowsOnlyTheMailboxIdentity(t *testing.T) {
	mod := &Module{}
	u, v := astral.GenerateIdentity(), astral.GenerateIdentity()

	for _, c := range []struct {
		name          string
		actor, target *astral.Identity
		allow         bool
	}{
		{"own mailbox", u, u, true},
		{"another mailbox", u, v, false},
		{"zero mailbox", &astral.Identity{}, &astral.Identity{}, false},
		{"zero actor", &astral.Identity{}, u, false},
	} {
		got := mod.AuthorizeHostMailbox(nil, &messaging.HostMailboxAction{
			Action:    auth.NewAction(c.actor),
			MailboxID: c.target,
		})
		if got != c.allow {
			t.Fatalf("%v: got %v, want %v", c.name, got, c.allow)
		}
	}
}

// A contract U→N permitting host_mailbox_action lets N host U's mailbox. The
// chain walk reaches the root rule as U, a hop from the node, so this also
// shows the root is not node-local.
func TestAContractFromTheMailboxIdentityHostsItsMailbox(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)

	if !askHosting(mod, mod.node.Identity(), u) {
		t.Fatal("the authority refuses the node hosting U under U's contract")
	}
	if !mod.hosts(u) {
		t.Fatal("the node does not host U's mailbox under U's contract")
	}
}

// The contract U issued does not host V's mailbox, even when an index row for
// V names it.
func TestTheSameContractDoesNotHostAnotherMailbox(t *testing.T) {
	mod := testMessagingModule(t)
	u, v := hostedParticipant(t, mod), keysOf(mod).mint()
	entry, _ := mod.mailboxes.Get(u.String())
	mustRecord(t, mod, v, entry)

	if askHosting(mod, mod.node.Identity(), v) {
		t.Fatal("the authority lets U's contract host V's mailbox")
	}
	if mod.hosts(v) {
		t.Fatal("the node hosts V's mailbox under U's contract")
	}
	if !mod.hosts(u) {
		t.Fatal("U's own mailbox stopped being hosted")
	}
}

// A hosting contract U issued to another node does not authorize this one,
// and neither does a contract this node issues to itself.
func TestAContractNamingAnotherNodeDoesNotAuthorize(t *testing.T) {
	mod := testMessagingModule(t)
	node := mod.node.Identity()
	u, other := keysOf(mod).mint(), keysOf(mod).mint()
	later := time.Now().Add(time.Hour)

	elsewhere := signContract(t, mod, newContract(u, other, later, hostingPermit()))
	if err := authorityOf(mod).IndexContract(mod.ctx, elsewhere); err != nil {
		t.Fatalf("index: %v", err)
	}
	self := signContract(t, mod, newContract(node, node, later, hostingPermit()))
	if err := authorityOf(mod).IndexContract(mod.ctx, self); err != nil {
		t.Fatalf("index: %v", err)
	}
	mustRecord(t, mod, u, entryNaming(t, elsewhere, later))

	if !askHosting(mod, other, u) {
		t.Fatal("the contract does not authorize the node it names; the check below proves nothing")
	}
	if askHosting(mod, node, u) || mod.hosts(u) {
		t.Fatal("a contract naming another node, or one the node issued itself, lets this node host U")
	}
}

// The relay contract create_identity signs grants routing, not hosting: an
// index row naming it serves nothing.
func TestRelayAuthorityAloneDoesNotHost(t *testing.T) {
	mod := testMessagingModule(t)

	u, err := mod.mintIdentity(mod.ctx)
	if err != nil {
		t.Fatalf("mint identity: %v", err)
	}
	relay := lastStoredContract(t, mod)
	if len(relay.HasPermit(nodes.RelayForAction{}.ObjectType())) != 1 || !relay.Issuer.IsEqual(u) {
		t.Fatal("the last contract mint_identity stored is not U's relay contract")
	}
	mustRecord(t, mod, u, entryNaming(t, relay, relay.ExpiresAt.Time()))

	if askHosting(mod, mod.node.Identity(), u) || mod.hosts(u) {
		t.Fatal("relay authority alone lets the node host U's mailbox")
	}
}

// A permit carrying any constraint does not authorize hosting: the action
// knows no constraint, so every one is refused rather than ignored.
func TestAConstrainedHostingPermitDoesNotAuthorize(t *testing.T) {
	mod := testMessagingModule(t)
	u := keysOf(mod).mint()
	later := time.Now().Add(time.Hour)

	sc := signContract(t, mod, newContract(u, mod.node.Identity(), later, hostingPermit(&astral.Ack{})))
	if err := authorityOf(mod).IndexContract(mod.ctx, sc); err != nil {
		t.Fatalf("index: %v", err)
	}
	mustRecord(t, mod, u, entryNaming(t, sc, later))

	if mod.hosts(u) {
		t.Fatal("a constrained hosting permit lets the node host U's mailbox")
	}
}

// A contract whose signature does not verify is refused by the auth index, so
// an index row naming it serves nothing.
func TestAnUnindexableContractDoesNotAuthorize(t *testing.T) {
	mod := testMessagingModule(t)
	u := keysOf(mod).mint()
	later := time.Now().Add(time.Hour)

	sc := signContract(t, mod, newContract(u, mod.node.Identity(), later, hostingPermit()))
	sc.ExpiresAt = astral.Time(later.Add(time.Hour)) // the signatures no longer cover it
	if err := authorityOf(mod).IndexContract(mod.ctx, sc); err == nil {
		t.Fatal("the auth index took a contract whose signatures do not verify")
	}
	mustRecord(t, mod, u, entryNaming(t, sc, later))

	if mod.hosts(u) {
		t.Fatal("an unindexable contract lets the node host U's mailbox")
	}
}

// An expired contract does not authorize, whatever expiry the index row
// claims; and a row that has expired does not serve, whatever the authority
// would still answer.
func TestAnExpiredContractDoesNotAuthorize(t *testing.T) {
	mod := testMessagingModule(t)
	u := keysOf(mod).mint()

	expired := signContract(t, mod, newContract(u, mod.node.Identity(), time.Now().Add(-time.Minute), hostingPermit()))
	if err := authorityOf(mod).IndexContract(mod.ctx, expired); err != nil {
		t.Fatalf("index: %v", err)
	}
	mustRecord(t, mod, u, entryNaming(t, expired, time.Now().Add(time.Hour)))

	if mod.hosts(u) {
		t.Fatal("an expired contract lets the node host U's mailbox")
	}

	v := hostedParticipant(t, mod)
	past := time.Now().Add(-time.Minute)
	entry, _ := mod.mailboxes.Get(v.String())
	entry.ExpiresAt = &past
	mod.mailboxes.Replace(v.String(), entry)

	if mod.hosts(v) {
		t.Fatal("an index row past its expiry still serves the mailbox")
	}
}

// The index cannot outlive the authority. The row claims ten years, the
// contract grants two seconds; once the contract expires the mailbox is
// neither routed to nor served — no delivery, no receipt, no listing — while
// the row and the stored mail stay.
func TestTheIndexDoesNotOutliveTheContract(t *testing.T) {
	mod := testMessagingModule(t)
	mod.config.HostingDuration = 2 * time.Second
	u := hostedParticipant(t, mod)
	outlast(t, mod, u)
	peer := astral.GenerateIdentity()
	early, late := messaging.NewMessageID(), messaging.NewMessageID()
	for _, id := range []messaging.MessageID{early, late} {
		mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: id, Sender: u, Recipient: peer, Content: "sent"})
	}

	if obj := deliverOverRouter(t, mod, u, &messaging.Message{ID: testID(1), Content: "kept"}); !isAck(obj) {
		t.Fatalf("a delivery while hosted answered %T, want an ack", obj)
	}
	if err := routeAndWrite(t, mod, receiptFrom(peer, u), &messaging.Receipt{ID: early}); err != nil {
		t.Fatalf("a receipt while hosted: %v", err)
	}
	if row := mustReadOwn(t, mod, u, messageRef{Box: messaging.BoxOutbox, ID: early}); row.FetchedAt == nil {
		t.Fatal("a receipt while hosted did not stamp the outbox row")
	}

	deadline := time.Now().Add(10 * time.Second)
	for mod.hosts(u) {
		if time.Now().After(deadline) {
			t.Fatal("the mailbox is still hosted long after its contract expired")
		}
		time.Sleep(50 * time.Millisecond)
	}

	_, err := mod.RouteQuery(mod.ctx, inFlight(u, messaging.MethodMessage), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("a delivery after expiry: got %v, want route not found", err)
	}
	err = routeAndWrite(t, mod, receiptFrom(peer, u), &messaging.Receipt{ID: late})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("a receipt after expiry: got %v, want route not found", err)
	}
	if row := mustReadOwn(t, mod, u, messageRef{Box: messaging.BoxOutbox, ID: late}); row.FetchedAt != nil {
		t.Fatal("a receipt after expiry stamped the outbox row")
	}
	if _, err = mod.ListMessages(context.Background(), u, messaging.ListMessagesRequest{}); !errors.Is(err, errNotParticipant) {
		t.Fatalf("a listing after expiry: got %v, want %v", err, errNotParticipant)
	}
	if _, err = mod.db.FindMailbox(u); err != nil {
		t.Fatalf("the index row went with the authority: %v", err)
	}
	if n := countOwned(t, mod.db, u); n != 3 {
		t.Fatalf("%v stored messages after expiry, want the 1 delivered and the 2 sent — expiry deletes no mail", n)
	}
}

// receiptFrom is a receipt query from the recipient of a message to its
// sender, as it arrives over a link.
func receiptFrom(recipient, sender *astral.Identity) *astral.InFlightQuery {
	q := astral.Launch(astral.NewQuery(recipient, sender, messaging.MethodReceipt))
	q.Extra.Set("origin", astral.OriginNetwork)
	return q
}

// outlast makes the identity's index row claim ten years, whatever its
// contract grants.
func outlast(t *testing.T, mod *Module, identity *astral.Identity) {
	t.Helper()

	far := time.Now().Add(10 * 365 * 24 * time.Hour).UTC()
	err := mod.db.Model(&dbMailbox{}).Where("identity = ?", identity).Update("expires_at", far).Error
	if err != nil {
		t.Fatalf("extend the row: %v", err)
	}
	entry, _ := mod.mailboxes.Get(identity.String())
	entry.ExpiresAt = &far
	mod.mailboxes.Replace(identity.String(), entry)
}

// lastStoredContract answers the signed contract the module stored last.
func lastStoredContract(t *testing.T, mod *Module) *auth.SignedContract {
	t.Helper()

	stored := mod.Objects.(*stubObjects).stored
	for i := len(stored) - 1; i >= 0; i-- {
		if sc, ok := stored[i].(*auth.SignedContract); ok {
			return sc
		}
	}
	t.Fatal("the module stored no contract")
	return nil
}

func isAck(obj astral.Object) bool {
	_, ok := obj.(*astral.Ack)
	return ok
}

// Hosting and correspondence are separate questions. With both mailboxes hosted
// and neither send nor receive permitted, a delivery is refused on either side;
// once both are permitted, it lands.
func TestHostingWithoutPermissionToCorrespondRefusesDelivery(t *testing.T) {
	mod := testMessagingModule(t)
	authority := authorityOf(mod)
	mod.Auth = authority
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	_, err := mod.RouteQuery(mod.ctx, inFlight(b, messaging.MethodMessage), &bufWriteCloser{})
	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) || rejected.Code != messaging.RejectNotAdmitted {
		t.Fatalf("a delivery to a hosted mailbox with no receive permission: got %v, want not admitted", err)
	}

	req := &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "hello"}
	if _, err = mod.SendMessage(context.Background(), a, req); err == nil || !strings.Contains(err.Error(), "unknown recipient") {
		t.Fatalf("a send with no send permission: got %v, want the recipient refused", err)
	}

	authority.Add(
		authmod.Func[*messaging.SendAction](func(*astral.Context, *messaging.SendAction) bool { return true }),
		authmod.Func[*messaging.ReceiveAction](func(*astral.Context, *messaging.ReceiveAction) bool { return true }),
	)
	if _, err = mod.SendMessage(context.Background(), a, req); err != nil {
		t.Fatalf("a send with both permissions: %v", err)
	}
}

// Hosting a mailbox opens it to nobody but its owner. The node holds hosting
// authority for every mailbox here, and still reads none; one participant
// reads nothing of another's.
func TestHostingDoesNotOpenAMailboxToAnotherCaller(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: b, Recipient: a, Content: "a's"})
	refs := &messaging.ReadMessagesRequest{Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: id}}}

	res, err := mod.ReadMessages(context.Background(), b, refs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 0 || len(res.NotFound) != 1 {
		t.Fatalf("b reading a's message answered %+v, want it not found", res)
	}

	if _, err = mod.ReadMessages(context.Background(), mod.node.Identity(), refs); !errors.Is(err, errNotParticipant) {
		t.Fatalf("the node reading a's message: got %v, want %v", err, errNotParticipant)
	}

	w := newRecordingWriter()
	err = routeQuery(t, mod.OpListMessages, originQuery(mod.node.Identity(), "messaging.list_messages", astral.OriginLocal), w)
	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) || w.written() != 0 {
		t.Fatalf("the node listing over the op: got %v and %v bytes, want a rejection", err, w.written())
	}
}

// The mail methods mod/mcp's tools call and the messaging.* operations ask one
// hosting check. For a mailbox the index names under a contract that does not
// authorize, every method answers errNotParticipant and every operation
// refuses, and each asked auth the same hosting question. read_messages learns
// the mailbox from its request, so it answers errNotParticipant once the
// request arrives.
func TestTheToolPathAndTheOpPathShareTheHostingCheck(t *testing.T) {
	mod := testMessagingModule(t)
	u := relayOnlyParticipant(t, mod)

	var tools messagingmod.Module = mod
	for name, call := range toolCalls(tools, u) {
		if err := call(); !errors.Is(err, errNotParticipant) {
			t.Fatalf("tool path %v: got %v, want %v", name, err, errNotParticipant)
		}
		checkLastHostingQuestion(t, mod, u, name)
	}

	for _, op := range mailOps() {
		res := tryOp(t, op.op(mod), originQuery(u, op.name+op.args, astral.OriginLocal), op.body)
		checkUnhosted(t, op, res, true)
		checkLastHostingQuestion(t, mod, u, op.name)
	}
}

// toolCalls answers one call per mail method of the public interface, each on
// behalf of owner.
func toolCalls(m messagingmod.Module, owner *astral.Identity) map[string]func() error {
	ctx := context.Background()
	ref := messaging.MessageRef{Box: messaging.BoxInbox, ID: messaging.NewMessageID()}

	return map[string]func() error{
		"SendMessage": func() error {
			_, err := m.SendMessage(ctx, owner, &messaging.SendMessageRequest{To: "anyone", Content: "x"})
			return err
		},
		"ListMessages": func() error {
			_, err := m.ListMessages(ctx, owner, messaging.ListMessagesRequest{})
			return err
		},
		"ReadMessages": func() error {
			_, err := m.ReadMessages(ctx, owner, &messaging.ReadMessagesRequest{Refs: []*messaging.MessageRef{&ref}})
			return err
		},
		"Wait": func() error {
			_, err := m.Wait(ctx, owner, messaging.WaitRequest{Timeout: time.Millisecond}, nil)
			return err
		},
		"Archive": func() error {
			_, err := m.Archive(ctx, owner, ref, false)
			return err
		},
	}
}

// checkLastHostingQuestion asserts that the last question auth was asked is
// whether this node hosts owner's mailbox.
func checkLastHostingQuestion(t *testing.T, mod *Module, owner *astral.Identity, path string) {
	t.Helper()

	asked := mod.Auth.(*fakeAuth).questions()
	if len(asked) == 0 {
		t.Fatalf("%v asked auth nothing", path)
	}
	hosting, ok := asked[len(asked)-1].(*messaging.HostMailboxAction)
	if !ok || !hosting.MailboxID.IsEqual(owner) || !hosting.Actor().IsEqual(mod.node.Identity()) {
		t.Fatalf("%v last asked %T, want whether the node hosts %v", path, asked[len(asked)-1], owner)
	}
}

// A fetch stamps the sender's outbox row directly only while the sender's
// mailbox is hosted here. A sender this node does not host is owed a receipt:
// one the index does not name, and one whose index row names a contract that
// does not authorize hosting.
func TestAFetchStampsTheSenderDirectlyOnlyWhileItIsHosted(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	id, err := mod.SendMessage(context.Background(), a, &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "x"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	readRef(t, mod, b, id)

	if sent := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxOutbox, ID: id}); sent.FetchedAt == nil {
		t.Fatal("a hosted sender's row was not stamped fetched")
	}

	for name, sender := range map[string]*astral.Identity{
		"not indexed":     keysOf(mod).mint(),
		"not authorizing": relayOnlyParticipant(t, mod),
	} {
		other := messaging.NewMessageID()
		mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: other, Sender: sender, Recipient: b, Content: "y"})
		mustInsertInbox(t, mod, &messaging.StoredMessage{ID: other, Sender: sender, Recipient: b, Content: "y"})
		readRef(t, mod, b, other)

		if sent := mustReadOwn(t, mod, sender, messageRef{Box: messaging.BoxOutbox, ID: other}); sent.FetchedAt != nil {
			t.Fatalf("a sender %v had its row stamped directly", name)
		}
		if got := mustReadOwn(t, mod, b, messageRef{Box: messaging.BoxInbox, ID: other}); got.ReceiptDueAt == nil {
			t.Fatalf("a sender %v was not owed a receipt", name)
		}
	}
}

// relayOnlyParticipant mints an identity whose index row names a contract it
// issued to this node that permits relaying and not hosting: a row the index
// holds as valid, under a contract that does not authorize.
func relayOnlyParticipant(t *testing.T, mod *Module) *astral.Identity {
	t.Helper()

	u := keysOf(mod).mint()
	relay := signContract(t, mod, newContract(u, mod.node.Identity(), time.Now().Add(time.Hour),
		&auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}))
	if err := authorityOf(mod).IndexContract(mod.ctx, relay); err != nil {
		t.Fatalf("index: %v", err)
	}
	mustRecord(t, mod, u, entryNaming(t, relay, time.Now().Add(time.Hour)))

	return u
}

// readRef reads one of the owner's inbox messages whole.
func readRef(t *testing.T, mod *Module, owner *astral.Identity, id messaging.MessageID) {
	t.Helper()

	_, err := mod.ReadMessages(context.Background(), owner, &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: id}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
}

// mustReadOwn answers the owner's stored row ref names, stamping nothing.
func mustReadOwn(t *testing.T, mod *Module, owner *astral.Identity, ref messageRef) *messaging.StoredMessage {
	t.Helper()

	var row dbMessage
	err := mod.db.Where("owner = ? AND box = ? AND id = ?", owner, ref.Box, ref.ID).Take(&row).Error
	if err != nil {
		t.Fatalf("row %v/%v of %v: %v", ref.Box, ref.ID, owner, err)
	}
	return row.stored()
}
