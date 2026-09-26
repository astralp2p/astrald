package messaging

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
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

// The in-memory index is loaded from the store, so a mailbox hosted before a
// restart is hosted after it, and not before the load.
func TestTheIndexIsLoadedFromTheStore(t *testing.T) {
	mod := testMessagingModule(t)
	identity := hostedParticipant(t, mod)

	restarted := &Module{Deps: mod.Deps, ctx: mod.ctx, db: mod.db, node: mod.node, config: mod.config, log: mod.log}
	if restarted.hosts(identity) {
		t.Fatal("the index answered before it was loaded")
	}

	if err := restarted.loadMailboxes(); err != nil {
		t.Fatalf("load mailboxes: %v", err)
	}
	if !restarted.hosts(identity) {
		t.Fatal("a stored mailbox is not hosted after a load")
	}
}
