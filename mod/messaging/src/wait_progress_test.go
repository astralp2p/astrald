package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A park reports on every floor interval it holds, and the numbers it carries
// are the window it spent and the window it was granted.
func TestAHeldParkReportsOnEveryFloor(t *testing.T) {
	mod := testMessagingModule(t)
	owner := astral.GenerateIdentity()

	q, err := mod.query(messaging.ListMessagesRequest{List: messaging.ListInbox})
	if err != nil {
		t.Fatal(err)
	}

	var spent []time.Duration
	var granted time.Duration

	// the reporter runs in the park's own goroutine and the park has returned
	// by the time these are read, so the slice needs no lock
	rows, err := mod.pollMessages(context.Background(), owner, pollRequest{
		Query:   q,
		Timeout: 120 * time.Millisecond,
		Floor:   20 * time.Millisecond,
		Report: func(s, g time.Duration) {
			spent = append(spent, s)
			granted = g
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows: got %v, want 0 on a quiet inbox", len(rows))
	}

	if len(spent) < 2 {
		t.Fatalf("reports: got %v across six floor intervals, want at least 2", len(spent))
	}
	if granted != 120*time.Millisecond {
		t.Fatalf("granted: got %v, want the window the park was given", granted)
	}
	for i := 1; i < len(spent); i++ {
		if spent[i] <= spent[i-1] {
			t.Fatalf("progress did not increase: %v then %v", spent[i-1], spent[i])
		}
	}
}

// An answer already waiting ends the park before its first floor, so nothing is
// reported on a call that never held.
func TestAnInstantAnswerReportsNothing(t *testing.T) {
	mod := testMessagingModule(t)
	owner, sender := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: sender, Recipient: owner, Content: "x"})

	q, err := mod.query(messaging.ListMessagesRequest{List: messaging.ListInbox})
	if err != nil {
		t.Fatal(err)
	}

	var reports int

	rows, err := mod.pollMessages(context.Background(), owner, pollRequest{
		Query:   q,
		Timeout: 120 * time.Millisecond,
		Floor:   20 * time.Millisecond,
		Report:  func(time.Duration, time.Duration) { reports++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: got %v, want the row already waiting", len(rows))
	}
	if reports != 0 {
		t.Fatalf("reports: got %v on a park that never held, want 0", reports)
	}
}
