package messaging

import (
	"math"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A peek of the caller's own mailbox hands the bodies out and stamps nothing:
// the row stays unread and its sender is never told it was collected.
func TestAPeekStampsNothing(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	id := mustSend(t, mod, a, &messaging.SendMessageRequest{To: astral.String8(b.String()), Content: "hello"})
	rows := snapshotRows(t, mod)

	req := &messaging.ReadMessagesRequest{Refs: []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: id}}}
	res := readOver(t, mod, localQuery(b, messaging.MethodReadMessages+"?peek=true"), req)
	if len(res.Messages) != 1 || res.Messages[0].Content == nil || *res.Messages[0].Content != "hello" {
		t.Fatalf("the peek answered %+v, want the one body", res.Messages)
	}
	if res.Messages[0].Envelope.ReadAt != nil {
		t.Fatal("the peek answered the row as read")
	}
	checkRowsUnchanged(t, mod, rows)

	readOver(t, mod, readQuery(b), req)
	if row := mustReadOwn(t, mod, b, messageRef{Box: messaging.BoxInbox, ID: id}); row.ReadAt == nil {
		t.Fatal("a read after the peek left the row unread")
	}
	if row := mustReadOwn(t, mod, a, messageRef{Box: messaging.BoxOutbox, ID: id}); row.FetchedAt == nil {
		t.Fatal("a read after the peek left the sender's row uncollected")
	}
}

// A peek is still a participant's read: a caller this node hosts no mailbox for
// is refused, as its own read is.
func TestAPeekOfAnUnhostedCallerIsRefused(t *testing.T) {
	mod := testMessagingModule(t)
	stranger := astral.GenerateIdentity()

	if _, err := mod.readFor(mod.ctx, stranger, &messaging.ReadMessagesRequest{}, true); err != errNotParticipant {
		t.Fatalf("the peek answered %v, want %v", err, errNotParticipant)
	}
}

// Paging by before answers every row once: each page is the newest rows below
// the position asked, and the next position is the page's smallest cursor.
func TestBeforePagesEachListWithoutAGapOrARepeat(t *testing.T) {
	mod := testMessagingModule(t)
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	for range 6 {
		mustInsertInbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "in"})
		mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "out"})
		mustInsertInbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: c, Recipient: a, Content: "other"})
	}

	for _, req := range []messaging.ListMessagesRequest{
		{List: messaging.ListInbox, From: b.String()},
		{List: messaging.ListOutbox, To: b.String()},
	} {
		t.Run(req.List, func(t *testing.T) {
			whole, err := mod.listMessages(a, messaging.ListMessagesRequest{List: req.List, From: req.From, To: req.To})
			if err != nil || len(whole) != 6 {
				t.Fatalf("the whole list: %v rows, err %v", len(whole), err)
			}

			var paged []uint64
			req.Limit = 2
			for pages := 0; ; pages++ {
				if pages > 4 {
					t.Fatal("paging did not end")
				}
				rows, err := mod.listMessages(a, req)
				if err != nil {
					t.Fatalf("page %v: %v", pages, err)
				}
				for _, row := range rows {
					paged = append(paged, uint64(row.Cursor))
				}
				if len(rows) < int(req.Limit) {
					break
				}
				req.Before = messaging.OldestCursor(envelopes(rows))
			}

			if len(paged) != 6 {
				t.Fatalf("paged %v rows, want 6: %v", len(paged), paged)
			}
			for i := 1; i < len(paged); i++ {
				if paged[i] >= paged[i-1] {
					t.Fatalf("the pages are not newest first without a repeat: %v", paged)
				}
			}
		})
	}
}

// since with limit drains the inbox forwards: oldest first, so a caller that
// takes the page's newest cursor as its next since misses nothing.
func TestSinceWithLimitKeepsTheInboxOldestFirst(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	for range 3 {
		mustInsertInbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "x"})
	}

	all, _ := mod.listMessages(a, messaging.ListMessagesRequest{List: messaging.ListInbox})
	first := uint64(all[0].Cursor)
	rows, err := mod.listMessages(a, messaging.ListMessagesRequest{List: messaging.ListInbox, Since: first, Limit: 1})
	if err != nil || len(rows) != 1 || uint64(rows[0].Cursor) != uint64(all[1].Cursor) {
		t.Fatalf("since %v limit 1 answered %+v, err %v; want the second row", first, rows, err)
	}

	newest, err := mod.listMessages(a, messaging.ListMessagesRequest{List: messaging.ListInbox, Limit: 1})
	if err != nil || len(newest) != 1 || uint64(newest[0].Cursor) != uint64(all[2].Cursor) {
		t.Fatalf("limit 1 answered %+v, err %v; want the newest row", newest, err)
	}
}

// A page that cannot mean what it asks is refused, rather than answered short.
func TestAPageThatCannotMeanAnythingIsRefused(t *testing.T) {
	mod := testMessagingModule(t)
	a := astral.GenerateIdentity()

	for _, c := range []struct {
		req  messaging.ListMessagesRequest
		want string
	}{
		{messaging.ListMessagesRequest{List: messaging.ListInbox, Since: 1, Before: 9}, "since and before page in opposite directions"},
		{messaging.ListMessagesRequest{List: messaging.ListArchive, Before: 9}, "before pages by cursor; the archive is read by time"},
		{messaging.ListMessagesRequest{List: messaging.ListInbox, Limit: 101}, "limit is at most 100, not 101"},
		{messaging.ListMessagesRequest{List: messaging.ListInbox, Before: math.MaxInt64 + 1}, "before is a cursor a previous answer gave you, not 9223372036854775808"},
	} {
		t.Run(c.want, func(t *testing.T) {
			_, err := mod.listMessages(a, c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("answered %v, want %q", err, c.want)
			}
		})
	}

	if _, err := mod.listMessages(a, messaging.ListMessagesRequest{List: messaging.ListInbox, Limit: 100}); err != nil {
		t.Fatalf("limit 100 was refused: %v", err)
	}
}
