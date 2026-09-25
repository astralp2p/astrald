package messaging

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// recordSQL runs fn and answers the statements the database was asked to run,
// as the driver received them. The plan a query gets is a property of the
// statement the code builds, so the statement has to come from the code.
func recordSQL(t *testing.T, db *DB, fn func(*DB)) []string {
	t.Helper()

	rec := &sqlRecorder{}
	fn(&DB{DB: db.Session(&gorm.Session{Logger: rec})})

	if len(rec.statements) == 0 {
		t.Fatal("the call issued no statement")
	}
	return rec.statements
}

type sqlRecorder struct {
	logger.Interface
	statements []string
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

// planOf answers the query plan SQLite chooses for a statement.
func planOf(t *testing.T, db *DB, sql string) string {
	t.Helper()

	var steps []struct{ Detail string }
	if err := db.Raw("EXPLAIN QUERY PLAN " + sql).Scan(&steps).Error; err != nil {
		t.Fatalf("explain: %v", err)
	}
	var out []string
	for _, s := range steps {
		out = append(out, s.Detail)
	}
	return strings.Join(out, " | ")
}

// An index nothing plans against costs writes and buys nothing. Each of these
// reads is the one its index was added for, and the assertion is on the plan
// rather than on a duration, which a loaded machine decides.
func TestTheHotReadsPlanAgainstTheirIndexes(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: id, Sender: b, Recipient: a, Content: "x"})

	for _, c := range []struct {
		name string
		want string
		call func(*DB)
	}{
		{
			// every inbound reply asks this before the row is stored
			name: "Holds",
			want: "ux_messaging__messages",
			call: func(db *DB) { _, _ = db.Holds(a, id) },
		},
		{
			name: "unread_only",
			want: "ix_messaging__messages_unread",
			call: func(db *DB) {
				_, _ = db.ListMessages(a, messageQuery{List: messaging.ListInbox, UnreadOnly: true})
			},
		},
		{
			name: "awaiting_pickup",
			want: "ix_messaging__messages_pickup",
			call: func(db *DB) {
				_, _ = db.ListMessages(a, messageQuery{List: messaging.ListOutbox, AwaitingPickup: true})
			},
		},
		{
			name: "archive",
			want: "ix_messaging__messages_archive",
			call: func(db *DB) { _, _ = db.ListMessages(a, messageQuery{List: messaging.ListArchive}) },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var plans []string
			for _, sql := range recordSQL(t, mod.db, c.call) {
				plan := planOf(t, mod.db, sql)
				if strings.Contains(plan, c.want) {
					return
				}
				plans = append(plans, plan)
			}
			t.Fatalf("no statement planned against %v: %v", c.want, strings.Join(plans, " ;; "))
		})
	}
}

// A listing must never fall to a temp b-tree: both orders are the index's to
// provide, and a sort over an unbounded listing is unbounded work.
func TestNoListingSortsInATempBTree(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "x",
	})

	for _, q := range []messageQuery{
		{List: messaging.ListInbox},
		{List: messaging.ListInbox, UnreadOnly: true},
		{List: messaging.ListOutbox},
		{List: messaging.ListArchive},
	} {
		for _, sql := range recordSQL(t, mod.db, func(db *DB) { _, _ = db.ListMessages(a, q) }) {
			if plan := planOf(t, mod.db, sql); strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("%+v sorts in a temp b-tree: %v", q, plan)
			}
		}
	}
}
