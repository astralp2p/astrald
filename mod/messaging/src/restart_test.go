package messaging

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A hosted mailbox survives a restart: the loader rebuilds the hosting index
// from the store, auth finds the hosting contract it indexed before, the mail
// stored before is still listed, and a delivery after the restart lands behind
// it.
func TestAHostedMailboxSurvivesARestart(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)
	if obj := deliverOverRouter(t, mod, u, &messaging.Message{ID: testID(1), Content: "before"}); !isAck(obj) {
		t.Fatalf("a delivery before the restart answered %T, want an ack", obj)
	}

	restarted := loadOver(t, mod.db, mod.node.Identity(), keysOf(mod))

	if !restarted.hosts(u) {
		t.Fatal("the mailbox is not hosted after the restart")
	}
	if obj := deliverOverRouter(t, restarted, u, &messaging.Message{ID: testID(2), Content: "after"}); !isAck(obj) {
		t.Fatalf("a delivery after the restart answered %T, want an ack", obj)
	}

	list, err := restarted.ListMessages(context.Background(), u, messaging.ListMessagesRequest{})
	if err != nil {
		t.Fatalf("list after the restart: %v", err)
	}
	if len(list) != 2 || list[0].ID != testID(1) || list[1].ID != testID(2) {
		t.Fatalf("the inbox after the restart holds %v messages, want the one from before and the one after", len(list))
	}
}

// The first start after the upgrade serves the mail mod/mcp held as it stood.
// Once Run provisions the imported mailbox, each list answers its own legacy
// rows with every stamp and seq, a reply still names the message it answers,
// and the next delivery takes the seq after the highest the legacy table ever
// issued.
func TestAnUpgradedMailboxServesItsCarriedOverMail(t *testing.T) {
	db, keys := openEmptyDB(t), newKeyring()
	nodeID, a := keys.mint(), keys.mint()
	rows := seedLegacy(t, db, a, astral.GenerateIdentity())
	legacySeq := sequenceOf(t, db, legacyMessages)

	mod := loadOver(t, db, nodeID, keys)
	mod.provisionPending(mod.ctx)
	if !mod.hosts(a) {
		t.Fatal("the imported mailbox is not hosted after Run")
	}

	carried := append(mustList(t, mod, a, messaging.ListInbox), mustList(t, mod, a, messaging.ListOutbox)...)
	carried = append(carried, mustList(t, mod, a, messaging.ListArchive)...)
	if len(carried) != len(rows) {
		t.Fatalf("the lists answer %v carried-over messages, want %v", len(carried), len(rows))
	}
	for _, row := range rows {
		checkCarried(t, envelopeOf(t, carried, row), row)
	}

	ask, answer := rows[0], rows[1]
	res, err := mod.ReadMessages(context.Background(), a, &messaging.ReadMessagesRequest{
		Refs:     []*messaging.MessageRef{{Box: messaging.BoxInbox, ID: ask.ID}},
		Children: messaging.ChildrenNone,
	})
	if err != nil || len(res.Messages) != 1 || len(res.Messages[0].ChildIDs) != 1 || res.Messages[0].ChildIDs[0] != answer.ID {
		t.Fatalf("the carried-over question answered %+v, err %v; want its answer as its one reply", res, err)
	}

	if obj := deliverOverRouter(t, mod, a, &messaging.Message{ID: testID(1), Content: "after"}); !isAck(obj) {
		t.Fatalf("a delivery after the upgrade answered %T, want an ack", obj)
	}
	inbox := mustList(t, mod, a, messaging.ListInbox)
	if next := inbox[len(inbox)-1]; next.ID != testID(1) || int64(next.Cursor) != legacySeq+1 {
		t.Fatalf("the first new message took seq %v, want %v", next.Cursor, legacySeq+1)
	}
}

// mustList answers one of the owner's lists.
func mustList(t *testing.T, mod *Module, owner *astral.Identity, list string) []*messaging.Envelope {
	t.Helper()

	envs, err := mod.ListMessages(context.Background(), owner, messaging.ListMessagesRequest{List: list})
	if err != nil {
		t.Fatalf("list %v: %v", list, err)
	}
	return envs
}

// envelopeOf answers the envelope among list that names row's box and id.
func envelopeOf(t *testing.T, list []*messaging.Envelope, row *dbMessage) *messaging.Envelope {
	t.Helper()

	for _, env := range list {
		if string(env.Box) == row.Box && env.ID == row.ID {
			return env
		}
	}
	t.Fatalf("no list answers the carried-over %v row %v", row.Box, row.ID)
	return nil
}

// checkCarried asserts the envelope answers the legacy row as it was written:
// its seq, parties, parent, every instant and its refusal words, where an
// empty refusal stays apart from none.
func checkCarried(t *testing.T, env *messaging.Envelope, row *dbMessage) {
	t.Helper()

	want := row.stored().Envelope()
	switch {
	case env.Cursor != astral.Uint64(row.Seq):
		t.Fatalf("%v: cursor %v, want the legacy seq %v", row.Content, env.Cursor, row.Seq)
	case !env.Sender.IsEqual(want.Sender) || !env.Recipient.IsEqual(want.Recipient) || env.ParentID != want.ParentID:
		t.Fatalf("%v: the parties or the parent changed on the way over", row.Content)
	case !env.CreatedAt.Time().Equal(want.CreatedAt.Time()):
		t.Fatalf("%v: created_at %v, want %v", row.Content, env.CreatedAt, want.CreatedAt)
	case (env.Err == nil) != (want.Err == nil) || env.Err != nil && *env.Err != *want.Err:
		t.Fatalf("%v: err %v, want %v", row.Content, env.Err, want.Err)
	}

	for name, pair := range map[string][2]*astral.Time{
		"archived_at":       {env.ArchivedAt, want.ArchivedAt},
		"read_at":           {env.ReadAt, want.ReadAt},
		"receipt_due_at":    {env.ReceiptDueAt, want.ReceiptDueAt},
		"receipt_stored_at": {env.ReceiptStoredAt, want.ReceiptStoredAt},
		"landed_at":         {env.LandedAt, want.LandedAt},
		"failed_at":         {env.FailedAt, want.FailedAt},
		"fetched_at":        {env.FetchedAt, want.FetchedAt},
	} {
		if got, w := pair[0], pair[1]; (got == nil) != (w == nil) || got != nil && !got.Time().Equal(w.Time()) {
			t.Fatalf("%v: %v %v, want %v", row.Content, name, got, w)
		}
	}
}
