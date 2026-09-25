package messaging

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// The mail operations carry a conversation between two hosted participants end
// to end: a send lands in the recipient's inbox, a listing answers it, a read
// hands out its body and stamps the sender's row collected, a reply names it,
// and an archive puts it away once.
func TestTheMailOperationsCarryAConversation(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	ask := sendOver(t, mod, localQuery(a, messaging.MethodSendMessage), &messaging.SendMessageRequest{
		To: astral.String8(b.String()), Content: "ask",
	})

	inbox := listOver(t, mod, localQuery(b, messaging.MethodListMessages))
	if len(inbox) != 1 || inbox[0].ID != ask || !inbox[0].Sender.IsEqual(a) {
		t.Fatalf("b's inbox over the op: %v messages, want a's one", len(inbox))
	}

	res := readOver(t, mod, localQuery(b, messaging.MethodReadMessages), &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: ask}},
	})
	if len(res.Messages) != 1 || res.Messages[0].Content == nil || *res.Messages[0].Content != "ask" {
		t.Fatalf("b's read over the op answered %+v, want a's message whole", res)
	}
	sent := listOver(t, mod, localQuery(a, messaging.MethodListMessages+"?list=outbox"))
	if len(sent) != 1 || sent[0].FetchedAt == nil {
		t.Fatal("a's sent row is not stamped collected once b read it")
	}

	answer := sendOver(t, mod, localQuery(b, messaging.MethodSendMessage), &messaging.SendMessageRequest{
		To: astral.String8(a.String()), Content: "answer", ParentID: ask,
	})
	inbox = listOver(t, mod, localQuery(a, messaging.MethodListMessages))
	if len(inbox) != 1 || inbox[0].ID != answer || inbox[0].ParentID != ask {
		t.Fatalf("a's inbox over the op: %v messages, want b's answer naming a's message", len(inbox))
	}

	ref := messaging.MethodArchive + "?box=inbox&id=" + ask.String()
	if !archiveOver(t, mod, localQuery(b, ref)) || archiveOver(t, mod, localQuery(b, ref)) {
		t.Fatal("an archive over the op must move the message once and then report it moved nothing")
	}
	archived := listOver(t, mod, localQuery(b, messaging.MethodListMessages+"?list=archive"))
	if len(archived) != 1 || archived[0].ID != ask {
		t.Fatalf("b's archive over the op holds %v messages, want the one put away", len(archived))
	}
}

// No argument a caller adds reaches another participant's mailbox. Every mail
// operation acts on the caller's own boxes, whatever owner, sender or identity
// the query names, and a send is the caller's whatever it claims.
func TestNoMailOperationActsOnAnotherMailbox(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	held := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: held, Sender: b, Recipient: a, Content: "a's"})
	claims := "owner=" + a.String() + "&sender=" + a.String() + "&identity=" + a.String()

	res := readOver(t, mod, localQuery(b, messaging.MethodReadMessages+"?"+claims), &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: held}},
	})
	if len(res.Messages) != 0 || len(res.NotFound) != 1 {
		t.Fatalf("b read a's message by naming a: %+v", res)
	}
	if archiveOver(t, mod, localQuery(b, messaging.MethodArchive+"?box=inbox&id="+held.String()+"&"+claims)) {
		t.Fatal("b archived a's message by naming a")
	}
	waited := waitOver(t, mod, localQuery(b, messaging.MethodWait+"?timeout=20ms&"+claims))
	if !waited.TimedOut || len(waited.Messages) != 0 {
		t.Fatalf("b's wait answered a's inbox by naming a: %+v", waited)
	}
	if row := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxInbox, ID: held}); row.ReadAt != nil || row.ArchivedAt != nil {
		t.Fatal("an operation b called stamped a's row")
	}

	sent := sendOver(t, mod, localQuery(b, messaging.MethodSendMessage+"?"+claims), &messaging.SendMessageRequest{
		To: astral.String8(a.String()), Content: "b's",
	})
	if row := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxInbox, ID: sent}); !row.Sender.IsEqual(b) {
		t.Fatalf("a send b called by naming a reached a from %v", row.Sender)
	}
	var claimed int64
	err := mod.db.Model(&dbMessage{}).Where("owner = ? AND box = ?", a, messaging.BoxOutbox).Count(&claimed).Error
	if err != nil || claimed != 0 {
		t.Fatalf("a's outbox holds %v rows after b's send, err %v; want none", claimed, err)
	}
}

// No argument of a mail operation and no field of a request body one reads
// can name a participant: the caller is the only mailbox an operation acts on.
func TestNoMailArgumentNamesAParticipant(t *testing.T) {
	names := map[string]bool{"Owner": true, "Sender": true, "Caller": true, "Identity": true, "Recipient": true, "Mailbox": true}
	identity := reflect.TypeFor[*astral.Identity]()

	for _, typ := range []reflect.Type{
		reflect.TypeFor[opSendMessageArgs](),
		reflect.TypeFor[opListMessagesArgs](),
		reflect.TypeFor[opReadMessagesArgs](),
		reflect.TypeFor[opWaitArgs](),
		reflect.TypeFor[opArchiveArgs](),
		reflect.TypeFor[messaging.SendMessageRequest](),
		reflect.TypeFor[messaging.ReadMessagesRequest](),
		reflect.TypeFor[messaging.MessageRef](),
	} {
		for i := range typ.NumField() {
			if f := typ.Field(i); f.Type == identity || names[f.Name] {
				t.Fatalf("%v.%v names a participant; a mail operation takes its mailbox from the caller alone", typ.Name(), f.Name)
			}
		}
	}
}

// onlyAnswer asserts the op answered exactly one object, of type T, and
// answers it. An error answer fails with its words.
func onlyAnswer[T astral.Object](t *testing.T, objs []astral.Object) T {
	t.Helper()

	if len(objs) != 1 {
		t.Fatalf("the op answered %v objects, want one", len(objs))
	}
	if e, ok := objs[0].(astral.Error); ok {
		t.Fatalf("the op answered an error: %v", e.Error())
	}
	answer, ok := objs[0].(T)
	if !ok {
		t.Fatalf("the op answered %T", objs[0])
	}
	return answer
}

// localQuery is a query from a local caller, the origin every mail operation
// serves.
func localQuery(caller *astral.Identity, queryString string) *astral.InFlightQuery {
	return originQuery(caller, queryString, astral.OriginLocal)
}

// sendOver sends one message through messaging.send_message.
func sendOver(t *testing.T, mod *Module, q *astral.InFlightQuery, req *messaging.SendMessageRequest) messaging.MessageID {
	t.Helper()
	return *onlyAnswer[*messaging.MessageID](t, callOp(t, mod.OpSendMessage, q, req))
}

// listOver answers one list through messaging.list_messages, which ends the
// stream with eos.
func listOver(t *testing.T, mod *Module, q *astral.InFlightQuery) []*messaging.Envelope {
	t.Helper()

	objs := collectOp(t, mod.OpListMessages, q)
	if len(objs) == 0 {
		t.Fatal("the listing answered nothing, not even eos")
	}
	if _, ok := objs[len(objs)-1].(*astral.EOS); !ok {
		t.Fatalf("the listing ended with %T, want eos", objs[len(objs)-1])
	}

	list := make([]*messaging.Envelope, 0, len(objs)-1)
	for _, obj := range objs[:len(objs)-1] {
		env, ok := obj.(*messaging.Envelope)
		if !ok {
			t.Fatalf("the listing answered %T, want envelopes", obj)
		}
		list = append(list, env)
	}
	return list
}

// readOver reads through messaging.read_messages.
func readOver(t *testing.T, mod *Module, q *astral.InFlightQuery, req *messaging.ReadMessagesRequest) *messaging.ReadMessagesResult {
	t.Helper()
	return onlyAnswer[*messaging.ReadMessagesResult](t, callOp(t, mod.OpReadMessages, q, req))
}

// archiveOver archives through messaging.archive, and answers whether the call
// moved the message.
func archiveOver(t *testing.T, mod *Module, q *astral.InFlightQuery) bool {
	t.Helper()
	return bool(onlyAnswer[*messaging.ArchiveResult](t, collectOp(t, mod.OpArchive, q)).Changed)
}

// waitOver parks through messaging.wait.
func waitOver(t *testing.T, mod *Module, q *astral.InFlightQuery) *messaging.WaitResult {
	t.Helper()
	return onlyAnswer[*messaging.WaitResult](t, collectOp(t, mod.OpWait, q))
}
