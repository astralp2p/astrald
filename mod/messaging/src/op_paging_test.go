package messaging

import (
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// pageOver answers the one object a paging op sends before its eos, failing on
// an error, a second object or a missing eos.
func pageOver[T astral.Object](t *testing.T, fn any, q *astral.InFlightQuery) T {
	t.Helper()

	objs := collectOp(t, fn, q)
	if len(objs) != 2 {
		t.Fatalf("%v answered %v objects, want the answer and its eos", q.QueryString, len(objs))
	}
	if _, ok := objs[1].(*astral.EOS); !ok {
		t.Fatalf("%v ended with %T, want eos", q.QueryString, objs[1])
	}
	return onlyAnswer[T](t, objs[:1])
}

// errorOver answers the words of the one error a paging op sends instead of a
// page.
func errorOver(t *testing.T, fn any, q *astral.InFlightQuery) string {
	t.Helper()

	objs := collectOp(t, fn, q)
	if len(objs) != 1 {
		t.Fatalf("%v answered %v objects, want one error", q.QueryString, len(objs))
	}
	e, ok := objs[0].(*astral.ErrorMessage)
	if !ok {
		t.Fatalf("%v answered %T, want an error", q.QueryString, objs[0])
	}
	return e.Error()
}

func TestAConversationPagesAndFollowsOverTheOps(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	for _, text := range []string{"one", "two", "three"} {
		sendOver(t, mod, localQuery(b, messaging.MethodSendMessage), &messaging.SendMessageRequest{
			To: astral.String8(a.String()), Content: astral.String32(text),
		})
	}

	peer := "?peer=" + b.String()
	first := pageOver[*messaging.MessagePage](t, mod.OpPageMessages, localQuery(a, messaging.MethodPageMessages+peer+"&limit=2"))
	if len(first.Messages) != 2 || first.NextBefore == 0 || first.Rev == 0 {
		t.Fatalf("first page: %v messages, next %v, rev %v", len(first.Messages), first.NextBefore, first.Rev)
	}
	older := pageOver[*messaging.MessagePage](t, mod.OpPageMessages, localQuery(a, messaging.MethodPageMessages+peer+
		"&limit=2&before="+first.NextBefore.String()+"&generation="+first.Generation.String()))
	if len(older.Messages) != 1 || older.NextBefore != 0 {
		t.Fatalf("older page: %v messages, next %v; want the last one and the end", len(older.Messages), older.NextBefore)
	}

	// a's own read stamps the newest; the change read from the first page's
	// revision answers that row at its new state
	newest := first.Messages[0].Envelope
	readOver(t, mod, localQuery(a, messaging.MethodReadMessages), &messaging.ReadMessagesRequest{
		Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: newest.ID}},
	})
	changes := pageOver[*messaging.MessageChanges](t, mod.OpListMessageChanges, localQuery(a,
		messaging.MethodListMessageChanges+peer+"&since="+first.Rev.String()+"&generation="+first.Generation.String()))
	if len(changes.Messages) != 1 || changes.Messages[0].Envelope.ID != newest.ID || changes.Messages[0].Envelope.ReadAt == nil {
		t.Fatalf("changes: %v rows, want the newest read", len(changes.Messages))
	}

	conv := pageOver[*messaging.ConversationPage](t, mod.OpPageConversations, localQuery(a, messaging.MethodPageConversations+peer))
	if len(conv.Conversations) != 1 || conv.Conversations[0].Unread != 2 || conv.Conversations[0].Latest.ID != newest.ID {
		t.Fatal("the conversation with b does not hold two unread and the newest")
	}
	follow := pageOver[*messaging.ConversationChanges](t, mod.OpListConversationChanges, localQuery(a,
		messaging.MethodListConversationChanges+"?since="+first.Rev.String()+"&generation=0"))
	if len(follow.Conversations) != 1 || follow.Conversations[0].Unread != 2 {
		t.Fatal("the read is not a conversation change")
	}
}

// A request that cannot mean anything is answered an error, never a page that
// could be read as the end.
func TestAPagingRequestThatCannotMeanAnythingIsRefused(t *testing.T) {
	mod := testMessagingModule(t)
	a := hostedParticipant(t, mod)

	for q, want := range map[string]string{
		messaging.MethodPageMessages + "?before=5":                              "generation is required with a position",
		messaging.MethodPageMessages + "?before=5&generation=7":                 messaging.ErrGeneration,
		messaging.MethodPageMessages + "?limit=101":                             "limit is at most 100, not 101",
		messaging.MethodPageMessages + "?list=inbox&peer=" + a.String():         "list and peer are exclusive: peer reads both boxes",
		messaging.MethodPageMessages + "?list=sent":                             "no such list: sent",
		messaging.MethodPageMessages + "?before=9223372036854775808":            "before is a position a previous answer gave you, not 9223372036854775808",
		messaging.MethodListMessageChanges + "?since=0":                         "generation is required with a position",
		messaging.MethodListMessageChanges + "?generation=0&peer=nobody":        "unknown correspondent: nobody",
		messaging.MethodPageConversations + "?peer=" + a.String() + "&before=3": "peer names one conversation; before pages them all",
		messaging.MethodListConversationChanges + "?since=1":                    "generation is required with a position",
	} {
		var fn any
		switch {
		case hasPrefix(q, messaging.MethodListMessageChanges):
			fn = mod.OpListMessageChanges
		case hasPrefix(q, messaging.MethodPageConversations):
			fn = mod.OpPageConversations
		case hasPrefix(q, messaging.MethodListConversationChanges):
			fn = mod.OpListConversationChanges
		default:
			fn = mod.OpPageMessages
		}
		if got := errorOver(t, fn, localQuery(a, q)); got != want {
			t.Errorf("%v: answered %q, want %q", q, got, want)
		}
	}
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p && (len(s) == len(p) || s[len(p)] == '?')
}

// A delegated page and change read hand nothing out and stamp nothing.
func TestADelegatedPageChangesNothing(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)
	reader := astral.GenerateIdentity()
	sendOver(t, mod, localQuery(b, messaging.MethodSendMessage), &messaging.SendMessageRequest{
		To: astral.String8(a.String()), Content: "x",
	})
	withReadAuthority(t, mod, reader, a)
	rows := snapshotRows(t, mod)

	mailbox := "mailbox=" + a.String()
	page := pageOver[*messaging.MessagePage](t, mod.OpPageMessages, localQuery(reader, messaging.MethodPageMessages+"?"+mailbox))
	if len(page.Messages) != 1 || page.Messages[0].Envelope.ReadAt != nil {
		t.Fatal("the delegated page does not answer a's unread message")
	}
	pageOver[*messaging.MessageChanges](t, mod.OpListMessageChanges, localQuery(reader, messaging.MethodListMessageChanges+"?generation=0&"+mailbox))
	pageOver[*messaging.ConversationPage](t, mod.OpPageConversations, localQuery(reader, messaging.MethodPageConversations+"?"+mailbox))
	pageOver[*messaging.ConversationChanges](t, mod.OpListConversationChanges, localQuery(reader, messaging.MethodListConversationChanges+"?generation=0&"+mailbox))

	checkRowsUnchanged(t, mod, rows)
}

// A reader the authority does not grant is refused every paging op on another
// mailbox, as it is the listing.
func TestAnUngrantedReaderPagesNothing(t *testing.T) {
	mod := testRouterModuleWithAuth(t, &fakeAuth{allow: false})
	a, reader := hostedParticipant(t, mod), astral.GenerateIdentity()
	mailbox := "mailbox=" + a.String()
	rows := snapshotRows(t, mod)

	for name, c := range map[string]struct {
		fn any
		q  string
	}{
		"page":                 {mod.OpPageMessages, messaging.MethodPageMessages + "?" + mailbox},
		"changes":              {mod.OpListMessageChanges, messaging.MethodListMessageChanges + "?generation=0&" + mailbox},
		"conversations":        {mod.OpPageConversations, messaging.MethodPageConversations + "?" + mailbox},
		"conversation changes": {mod.OpListConversationChanges, messaging.MethodListConversationChanges + "?generation=0&" + mailbox},
	} {
		checkRefused(t, name, tryOp(t, c.fn, localQuery(reader, c.q), nil), false)
	}
	checkRowsUnchanged(t, mod, rows)
}
