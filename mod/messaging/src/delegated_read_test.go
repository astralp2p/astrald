package messaging

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
)

// A delegated read: a caller lists or reads a mailbox this node hosts for
// another identity, under mod.messaging.read_mailbox_action, and changes
// nothing in it.

// readPermit is an unconstrained permit for read_mailbox_action.
func readPermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(messaging.ReadMailboxAction{}.ObjectType())}
}

// askRead asks the authority directly whether reader may read mailbox's
// mailbox.
func askRead(mod *Module, reader, mailbox *astral.Identity) bool {
	return authorityOf(mod).Authorize(mod.ctx, &messaging.ReadMailboxAction{
		Action:    auth.NewAction(reader),
		MailboxID: mailbox,
	})
}

// mustIndex signs the contract with the keys the module's keyring holds and
// indexes it with the real auth module.
func mustIndex(t *testing.T, mod *Module, c *auth.Contract) {
	t.Helper()

	if err := authorityOf(mod).IndexContract(mod.ctx, signContract(t, mod, c)); err != nil {
		t.Fatalf("index: %v", err)
	}
}

// listQuery is a local caller's messaging.list_messages naming mailbox.
func listQuery(caller *astral.Identity, mailbox string) *astral.InFlightQuery {
	return localQuery(caller, messaging.MethodListMessages+"?mailbox="+mailbox)
}

// readQuery is a local caller's messaging.read_messages.
func readQuery(caller *astral.Identity) *astral.InFlightQuery {
	return localQuery(caller, messaging.MethodReadMessages)
}

// inboxRead is a request naming one inbox message of mailbox.
func inboxRead(mailbox *astral.Identity, id messaging.MessageID) *messaging.ReadMessagesRequest {
	return &messaging.ReadMessagesRequest{
		Refs:    []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: id}},
		Mailbox: mailbox,
	}
}

// readAuthority is an external authority for read_mailbox_action that grants
// exactly the (reader, mailbox) pairs it names, and records every actor it was
// asked about.
type readAuthority struct {
	grants map[[2]string]bool
	asked  []*astral.Identity
}

func (a *readAuthority) Ask(_ *astral.Context, action auth.ActionObject) (bool, error) {
	read, ok := action.(*messaging.ReadMailboxAction)
	if !ok {
		return false, errors.New("not a read_mailbox_action")
	}
	a.asked = append(a.asked, read.Actor())
	return a.grants[[2]string{read.Actor().String(), read.MailboxID.String()}], nil
}

// withReadAuthority registers an external authority for read_mailbox_action
// with the real auth module, granting reader each mailbox named.
func withReadAuthority(t *testing.T, mod *Module, reader *astral.Identity, mailboxes ...*astral.Identity) *readAuthority {
	t.Helper()

	authority := &readAuthority{grants: map[[2]string]bool{}}
	for _, m := range mailboxes {
		authority.grants[[2]string{reader.String(), m.String()}] = true
	}

	action := messaging.ReadMailboxAction{}.ObjectType()
	if err := authorityOf(mod).(*authsrc.Module).AddExternal(action, authsrc.ExternalConfig{}, authority); err != nil {
		t.Fatalf("add external: %v", err)
	}
	return authority
}

// mustForgeRead signs a contract issuer→subject permitting read_mailbox_action
// with the keys this node holds, as auth.sign_contract does for any caller,
// indexes it, and checks the index answers it for the action.
func mustForgeRead(t *testing.T, mod *Module, issuer, subject *astral.Identity, delegation uint8) {
	t.Helper()

	p := readPermit()
	p.Delegation = astral.Uint8(delegation)
	mustIndex(t, mod, newContract(issuer, subject, time.Now().Add(time.Hour), p))

	found, err := authorityOf(mod).SignedContracts().
		WithIssuer(issuer).
		WithSubject(subject).
		WithAction(&messaging.ReadMailboxAction{}).
		Find(mod.ctx)
	if err != nil || len(found) == 0 {
		t.Fatalf("the forged contract is not indexed for the read: %v contracts, err %v", len(found), err)
	}
}

// No rule answers read_mailbox_action: with this module's rules registered and
// no external authority, the real auth module refuses every read, the mailbox
// identity's read of its own mailbox included.
func TestNoRuleGrantsARead(t *testing.T) {
	mod := testMessagingModule(t)
	u, v := hostedParticipant(t, mod), hostedParticipant(t, mod)

	for _, c := range []struct {
		name          string
		actor, target *astral.Identity
	}{
		{"own mailbox", u, u},
		{"another mailbox", u, v},
		{"the hosting node", mod.node.Identity(), u},
		{"zero mailbox", &astral.Identity{}, &astral.Identity{}},
	} {
		if askRead(mod, c.actor, c.target) {
			t.Fatalf("%v: the authority granted the read", c.name)
		}
	}
}

// A contract from the mailbox identity to a reader, signed with the keys this
// node holds as auth.sign_contract signs for any caller and indexed, grants no
// read and no hosting, while the authority grants the mailbox identity its own
// read: the reader is refused, and the authority is never asked about U.
func TestAForgedContractFromTheMailboxIdentityGrantsNoRead(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)

	reader := keysOf(mod).mint()
	mustForgeRead(t, mod, u, reader, 0)
	authority := withReadAuthority(t, mod, u, u)

	if askRead(mod, reader, u) {
		t.Fatal("a contract from U lets R read U's mailbox")
	}
	if askedAbout(authority.asked, u) {
		t.Fatal("the authority was asked about U, the forged contract's issuer")
	}
	if askHosting(mod, reader, u) {
		t.Fatal("a read permit lets R host U's mailbox")
	}
	if !askRead(mod, u, u) {
		t.Fatal("the authority grants U no read of its own mailbox")
	}
}

// No contract carries a read, even from an identity the authority grants one:
// the authority grants A a read of X, and a local caller signs A→R and, with
// delegation, A→M→R2 with the keys this node holds. Through auth and through
// the operations, with the real auth module deciding, R and R2 are refused,
// the authority is asked about no one but them, and X's mailbox is unchanged.
// A itself still reads X.
func TestNoContractCarriesARead(t *testing.T) {
	mod := testMessagingModule(t)
	a, x := hostedParticipant(t, mod), hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: x, Content: "x's"})

	keys := keysOf(mod)
	r, m, r2 := keys.mint(), keys.mint(), keys.mint()
	mustForgeRead(t, mod, a, r, 0)
	mustForgeRead(t, mod, a, m, 1)
	mustForgeRead(t, mod, m, r2, 0)
	authority := withReadAuthority(t, mod, a, x)

	for _, reader := range []*astral.Identity{r, r2} {
		if askRead(mod, reader, x) {
			t.Fatalf("a contract chain from A lets %v read X's mailbox", reader)
		}
	}
	if askedAbout(authority.asked, a) || askedAbout(authority.asked, m) {
		t.Fatal("the authority was asked about a contract's issuer")
	}
	if !askRead(mod, a, x) {
		t.Fatal("the authority no longer grants A its read of X")
	}

	mod.Auth = authorityOf(mod)
	rows := snapshotRows(t, mod)
	for _, reader := range []*astral.Identity{r, r2} {
		checkRefused(t, "the listing", tryOp(t, mod.OpListMessages, listQuery(reader, x.String()), nil), false)
		checkRefused(t, "the read", tryOp(t, mod.OpReadMessages, readQuery(reader), inboxRead(x, id)), true)
	}
	checkRowsUnchanged(t, mod, rows)
}

// Through the operations, with the real auth module deciding and the authority
// granting the mailbox identity its own read: a reader holding a forged
// contract from the mailbox identity is refused its listing and its read, and
// nothing in the mailbox changes.
func TestTheOperationsRefuseAReaderHoldingAForgedContract(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: u, Content: "u's"})

	reader := keysOf(mod).mint()
	mustForgeRead(t, mod, u, reader, 0)
	withReadAuthority(t, mod, u, u)
	mod.Auth = authorityOf(mod)
	rows := snapshotRows(t, mod)

	checkRefused(t, "the listing", tryOp(t, mod.OpListMessages, listQuery(reader, u.String()), nil), false)
	checkRefused(t, "the read", tryOp(t, mod.OpReadMessages, readQuery(reader), inboxRead(u, id)), true)
	checkRowsUnchanged(t, mod, rows)
}

// The external authority grants a delegated read, and a contract adds nothing
// to it: through the operations, with the real auth module deciding, the reader
// the authority names lists and reads U's mailbox. A reader holding a forged
// contract from U is refused, and the authority is never asked about U: the
// action refuses every permit, so auth passes no contract link for the read.
func TestTheExternalAuthorityGrantsADelegatedRead(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: u, Content: "u's"})

	reader, forger := keysOf(mod).mint(), keysOf(mod).mint()
	mustForgeRead(t, mod, u, forger, 0)
	authority := withReadAuthority(t, mod, reader, u)
	mod.Auth = authorityOf(mod)

	if list := listOver(t, mod, listQuery(reader, u.String())); len(list) != 1 || list[0].ID != id {
		t.Fatalf("the reader listed %v rows of U's inbox, want the one", len(list))
	}
	res := readOver(t, mod, readQuery(reader), inboxRead(u, id))
	if len(res.Messages) != 1 || res.Messages[0].Content == nil || *res.Messages[0].Content != "u's" {
		t.Fatalf("the reader read %+v, want U's message whole", res)
	}

	checkRefused(t, "the forger's listing", tryOp(t, mod.OpListMessages, listQuery(forger, u.String()), nil), false)
	checkRefused(t, "the forger's read", tryOp(t, mod.OpReadMessages, readQuery(forger), inboxRead(u, id)), true)
	if askedAbout(authority.asked, u) {
		t.Fatal("the authority was asked about U, the forged contract's issuer")
	}
}

// askedAbout answers whether id is among the actors asked.
func askedAbout(asked []*astral.Identity, id *astral.Identity) bool {
	for _, a := range asked {
		if a.IsEqual(id) {
			return true
		}
	}
	return false
}

// A delegated read asks auth one question, mod.messaging.read_mailbox_action,
// with the caller as actor and the named mailbox as MailboxID — whether the
// listing names the mailbox by identity or by alias.
func TestADelegatedReadAsksAboutTheCallerAndTheMailbox(t *testing.T) {
	mod := testMessagingModule(t)
	u, reader := hostedParticipant(t, mod), astral.GenerateIdentity()
	mod.Dir.(*stubDir).aliases["ursula"] = u
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: u, Content: "x"})

	for name, call := range map[string]func(){
		"list by identity": func() { listOver(t, mod, listQuery(reader, u.String())) },
		"list by alias":    func() { listOver(t, mod, listQuery(reader, "ursula")) },
		"read":             func() { readOver(t, mod, readQuery(reader), inboxRead(u, id)) },
	} {
		t.Run(name, func(t *testing.T) {
			before := len(mod.Auth.(*fakeAuth).questions())
			call()
			checkReadAsked(t, readsAsked(mod, before), reader, u)
		})
	}
}

// readsAsked answers the read_mailbox_action questions auth was asked after the
// first before.
func readsAsked(mod *Module, before int) (reads []*messaging.ReadMailboxAction) {
	for _, q := range mod.Auth.(*fakeAuth).questions()[before:] {
		if r, ok := q.(*messaging.ReadMailboxAction); ok {
			reads = append(reads, r)
		}
	}
	return reads
}

// checkReadAsked asserts reads is one question: whether reader may read
// mailbox's mailbox.
func checkReadAsked(t *testing.T, reads []*messaging.ReadMailboxAction, reader, mailbox *astral.Identity) {
	t.Helper()

	if len(reads) != 1 {
		t.Fatalf("auth was asked %v read_mailbox_action questions, want one", len(reads))
	}
	if !reads[0].Actor().IsEqual(reader) || !reads[0].MailboxID.IsEqual(mailbox) {
		t.Fatalf("auth was asked about %v reading %v, want %v reading %v", reads[0].Actor(), reads[0].MailboxID, reader, mailbox)
	}
}

// A refused delegated read reads nothing: the listing is rejected, the read is
// accepted and ended with no answer, and the mailbox's rows are as they were.
func TestARefusedDelegatedReadReadsNothing(t *testing.T) {
	mod := testRouterModuleWithAuth(t, &fakeAuth{allow: false})
	u, reader := hostedParticipant(t, mod), astral.GenerateIdentity()
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: u, Content: "x"})
	rows := snapshotRows(t, mod)

	asked := len(mod.Auth.(*fakeAuth).questions())
	checkRefused(t, "the listing", tryOp(t, mod.OpListMessages, listQuery(reader, u.String()), nil), false)
	checkReadAsked(t, readsAsked(mod, asked), reader, u)

	asked = len(mod.Auth.(*fakeAuth).questions())
	req := inboxRead(u, id)
	req.Children = messaging.ChildrenFull
	checkRefused(t, "the read", tryOp(t, mod.OpReadMessages, readQuery(reader), req), true)
	checkReadAsked(t, readsAsked(mod, asked), reader, u)

	checkRowsUnchanged(t, mod, rows)
}

// A delegated read hands bodies out and stamps nothing, for the messages it
// names and for their replies under children full alike: no read_at, no
// receipt due, no receipt, and no fetched_at on a local sender's outbox row.
// The same read by the mailbox's own identity stamps every one of them, so the
// setup is one an own read changes.
func TestADelegatedReadChangesNothing(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	remote, reader := astral.GenerateIdentity(), astral.GenerateIdentity()

	first := mustSend(t, mod, a, &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "first"})
	asked := mustSend(t, mod, b, &messaging.SendMessageRequest{To: astral.String8(a.String()), Content: "asked"})
	reply := mustSend(t, mod, a, &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "reply", ParentID: asked})
	far := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: far, Sender: remote, Recipient: b, Content: "far"})
	req := &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{
			{Box: messaging.BoxInbox, ID: first}, {Box: messaging.BoxOutbox, ID: asked}, {Box: messaging.BoxInbox, ID: far},
		},
		Children: messaging.ChildrenFull,
		Mailbox:  b,
	}
	rows := snapshotRows(t, mod)

	if list := listOver(t, mod, listQuery(reader, b.String())); len(list) != 3 {
		t.Fatalf("the reader listed %v rows of b's inbox, want 3", len(list))
	}
	if list := listOver(t, mod, listQuery(reader, b.String()+"&list=outbox")); len(list) != 1 || list[0].ID != asked {
		t.Fatalf("the reader listed %v rows of b's outbox, want the one b sent", len(list))
	}
	checkHandedOutUnstamped(t, readOver(t, mod, readQuery(reader), req))
	checkRowsUnchanged(t, mod, rows)

	req.Mailbox = nil
	readOver(t, mod, readQuery(b), req)
	for _, id := range []messaging.MessageID{first, reply} {
		if row := mustReadOwn(t, mod, b, messageRef{Box: messaging.BoxInbox, ID: id}); row.ReadAt == nil {
			t.Fatalf("b's own read left %v unread", id)
		}
		if row := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxOutbox, ID: id}); row.FetchedAt == nil {
			t.Fatalf("b's own read left a's %v uncollected", id)
		}
	}
	if row := mustReadOwn(t, mod, b, messageRef{Box: messaging.BoxInbox, ID: far}); row.ReceiptDueAt == nil {
		t.Fatal("b's own read owed the remote sender no receipt")
	}
}

// checkHandedOutUnstamped asserts the read answered its three messages and the
// one reply whole, each unstamped.
func checkHandedOutUnstamped(t *testing.T, res *messaging.ReadMessagesResult) {
	t.Helper()

	if len(res.Messages) != 3 || len(res.Replies) != 1 || res.Replies[0].Content == nil || *res.Replies[0].Content != "reply" {
		t.Fatalf("the reader read %v messages and %v replies, want 3 whole and a's reply whole", len(res.Messages), len(res.Replies))
	}
	for _, m := range append(res.Messages, res.Replies...) {
		if m.Content == nil || m.Envelope.ReadAt != nil || m.Envelope.ReceiptDueAt != nil {
			t.Fatalf("the reader was answered %v with its body %v and read_at %v", m.Envelope.ID, m.Content != nil, m.Envelope.ReadAt)
		}
	}
}

// mustSend sends one message from a hosted sender.
func mustSend(t *testing.T, mod *Module, from *astral.Identity, req *messaging.SendMessageRequest) messaging.MessageID {
	t.Helper()

	id, err := mod.SendMessage(context.Background(), from, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	return id
}

// snapshotRows answers every stored row, in the order they were written.
func snapshotRows(t *testing.T, mod *Module) []dbMessage {
	t.Helper()

	var rows []dbMessage
	if err := mod.db.Order("seq").Find(&rows).Error; err != nil {
		t.Fatalf("rows: %v", err)
	}
	return rows
}

// checkRowsUnchanged asserts every stored row reads as it did in before.
func checkRowsUnchanged(t *testing.T, mod *Module, before []dbMessage) {
	t.Helper()

	after := snapshotRows(t, mod)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("the rows changed:\nbefore %+v\n after %+v", before, after)
	}
}

// A mailbox this node does not host is refused before auth is asked about the
// read: an identity the index does not name, one whose index row names a
// contract that does not authorize, and a name that resolves to nobody.
func TestADelegatedReadOfAnUnhostedMailboxIsRefused(t *testing.T) {
	mod := testMessagingModule(t)
	reader := astral.GenerateIdentity()

	for name, mailbox := range map[string]*astral.Identity{
		"not indexed":     keysOf(mod).mint(),
		"not authorizing": relayOnlyParticipant(t, mod),
	} {
		checkRefused(t, name+" listing", tryOp(t, mod.OpListMessages, listQuery(reader, mailbox.String()), nil), false)
		checkRefused(t, name+" read", tryOp(t, mod.OpReadMessages, readQuery(reader), inboxRead(mailbox, messaging.NewMessageID())), true)
	}
	checkRefused(t, "an unresolved name", tryOp(t, mod.OpListMessages, listQuery(reader, "nobody"), nil), false)

	if reads := readsAsked(mod, 0); len(reads) != 0 {
		t.Fatalf("auth was asked %v questions about reading a mailbox this node does not host", len(reads))
	}
}

// A delegated read is refused before anything is asked when it comes off a
// link or from an MCP agent, or when its caller is the zero identity or this
// node — even with the authority granting every read.
func TestADelegatedReadRefusesAnOriginOrACallerBeforeAsking(t *testing.T) {
	mod := testMessagingModule(t)
	u, reader := hostedParticipant(t, mod), astral.GenerateIdentity()
	authority := mod.Auth.(*fakeAuth)

	for _, c := range []struct {
		name   string
		caller *astral.Identity
		origin string
	}{
		{"network origin", reader, astral.OriginNetwork},
		{"mcp origin", reader, astral.OriginMCP},
		{"zero caller", &astral.Identity{}, astral.OriginLocal},
		{"node caller", mod.node.Identity(), astral.OriginLocal},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := len(authority.questions())

			list := originQuery(c.caller, messaging.MethodListMessages+"?mailbox="+u.String(), c.origin)
			checkRefused(t, "the listing", tryOp(t, mod.OpListMessages, list, nil), false)
			read := originQuery(c.caller, messaging.MethodReadMessages, c.origin)
			checkRefused(t, "the read", tryOp(t, mod.OpReadMessages, read, inboxRead(u, messaging.NewMessageID())), false)

			if asked := authority.questions()[before:]; len(asked) != 0 {
				t.Fatalf("auth was asked %v; want nothing", asked)
			}
		})
	}
}

// Naming the caller's own mailbox, by identity or alias, is the caller's own
// read: it asks auth nothing about reading, and it stamps as an own read does.
func TestNamingTheCallersOwnMailboxIsAnOwnRead(t *testing.T) {
	mod := testMessagingModule(t)
	b := hostedParticipant(t, mod)
	mod.Dir.(*stubDir).aliases["bea"] = b
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: astral.GenerateIdentity(), Recipient: b, Content: "b's"})

	for _, name := range []string{b.String(), "bea"} {
		if list := listOver(t, mod, listQuery(b, name)); len(list) != 1 {
			t.Fatalf("b listing its own mailbox as %v answered %v rows, want 1", name, len(list))
		}
	}
	readOver(t, mod, readQuery(b), inboxRead(b, id))

	if row := mustReadOwn(t, mod, b, messageRef{Box: messaging.BoxInbox, ID: id}); row.ReadAt == nil {
		t.Fatal("b's read of its own mailbox, named, did not stamp its row")
	}
	if reads := readsAsked(mod, 0); len(reads) != 0 {
		t.Fatalf("an own read asked auth %v questions about reading a mailbox", len(reads))
	}
}

// A caller whose mailbox this node does not host, reading its own, is refused
// once its request arrives, with errNotParticipant and no question to auth: the
// request is what names the mailbox read, and the mailbox is the caller's own.
// Naming its own mailbox changes nothing.
func TestAnUnhostedCallersOwnReadIsAnsweredNotAParticipant(t *testing.T) {
	mod := testMessagingModule(t)
	caller := astral.GenerateIdentity()

	checkNotParticipant(t, "the read", tryOp(t, mod.OpReadMessages, readQuery(caller), inboxRead(nil, messaging.NewMessageID())))
	checkNotParticipant(t, "the named read", tryOp(t, mod.OpReadMessages, readQuery(caller), inboxRead(caller, messaging.NewMessageID())))

	if asked := mod.Auth.(*fakeAuth).questions(); len(asked) != 0 {
		t.Fatalf("auth was asked %v; want nothing", asked)
	}
}

// The mail methods mod/mcp's tools call act on the mailbox they are handed: a
// request naming another is refused and reads nothing, and one naming the
// owner is the owner's own.
func TestAMailMethodRefusesAnotherMailbox(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: b, Recipient: a, Content: "a's"})
	ctx := context.Background()

	if _, err := mod.ListMessages(ctx, b, messaging.ListMessagesRequest{Mailbox: a.String()}); !errors.Is(err, errAnotherMailbox) {
		t.Fatalf("b listing a's mailbox through the method: got %v, want %v", err, errAnotherMailbox)
	}
	if _, err := mod.ReadMessages(ctx, b, inboxRead(a, id)); !errors.Is(err, errAnotherMailbox) {
		t.Fatalf("b reading a's mailbox through the method: got %v, want %v", err, errAnotherMailbox)
	}
	if row := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxInbox, ID: id}); row.ReadAt != nil {
		t.Fatal("a refused read stamped a's row")
	}

	if list, err := mod.ListMessages(ctx, a, messaging.ListMessagesRequest{Mailbox: a.String()}); err != nil || len(list) != 1 {
		t.Fatalf("a listing its own mailbox, named: %v rows, err %v", len(list), err)
	}
	if res, err := mod.ReadMessages(ctx, a, inboxRead(a, id)); err != nil || len(res.Messages) != 1 {
		t.Fatalf("a reading its own mailbox, named: %+v, err %v", res, err)
	}
}
