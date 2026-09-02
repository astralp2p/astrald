package mcp

import (
	"context"
	"testing"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// The grant is the ask under the ceiling, and the deployment's words fill in
// whatever the caller left out.
func TestTheGrantIsTheAskUnderTheCeiling(t *testing.T) {
	withWaits := func(def, max time.Duration) Config {
		c := defaultConfig
		if def > 0 {
			c.WaitDefault = def
		}
		if max > 0 {
			c.WaitMax = max
		}
		return c
	}

	cases := map[string]struct {
		config Config
		ask    time.Duration
		want   time.Duration
	}{
		"nothing named":              {withWaits(0, 0), 0, 2 * time.Minute},
		"an ask under the ceiling":   {withWaits(0, 0), time.Second, time.Second},
		"an ask over the ceiling":    {withWaits(0, 3*time.Second), time.Minute, 3 * time.Second},
		"a default over the ceiling": {withWaits(time.Minute, 3*time.Second), 0, 3 * time.Second},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mod := testMessageModule(t)
			mod.config = c.config
			owner, sender := astral.GenerateIdentity(), astral.GenerateIdentity()

			// a row is already waiting, so the park answers at once and the
			// grant is reported without being spent
			mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: sender, Recipient: owner, Content: "x"})

			ans, err := mod.waitMessages(context.Background(), owner, waitRequest{Timeout: c.ask})
			if err != nil {
				t.Fatal(err)
			}
			if ans.Granted != c.want {
				t.Fatalf("granted: got %v, want %v", ans.Granted, c.want)
			}
		})
	}
}

// An answer already waiting spends nothing of the window, and the spend is
// what says so.
func TestAnInstantAnswerSpendsNothing(t *testing.T) {
	mod := testMessageModule(t)
	owner, sender := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: sender, Recipient: owner, Content: "x"})

	ans, err := mod.waitMessages(context.Background(), owner, waitRequest{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Rows) != 1 {
		t.Fatalf("rows: got %v, want 1", len(ans.Rows))
	}
	if ans.Waited > 2*time.Second {
		t.Fatalf("waited %v on an answer that was already there", ans.Waited)
	}
}

// A timed-out answer keeps the caller's cursor and accounts its window: the
// grant is the ask, and the spend fills it.
func TestATimedOutAnswerKeepsTheCursor(t *testing.T) {
	mod := testMessageModule(t)
	owner, sender := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: sender, Recipient: owner, Content: "x"})

	wait := mod.waitTool(owner)

	_, first, err := wait(context.Background(), nil, waitIn{})
	if err != nil {
		t.Fatal(err)
	}
	if first.TimedOut || first.NextSince == "" {
		t.Fatalf("first wait: timed_out %v, next_since %q", first.TimedOut, first.NextSince)
	}
	if first.GrantedSecs != 120 {
		t.Fatalf("granted_secs: got %v, want the default 120", first.GrantedSecs)
	}

	_, again, err := wait(context.Background(), nil, waitIn{Since: first.NextSince, TimeoutSecs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !again.TimedOut {
		t.Fatal("a park past the cursor must time out")
	}
	if again.NextSince != first.NextSince {
		t.Fatalf("cursor: got %q, want the one sent, %q", again.NextSince, first.NextSince)
	}
	if again.GrantedSecs != 1 || again.WaitedSecs != 1 {
		t.Fatalf("window: granted %v waited %v, want 1 and 1", again.GrantedSecs, again.WaitedSecs)
	}
}

// A listing with a cursor and nothing newer keeps the cursor too.
func TestAListingKeepsTheCursor(t *testing.T) {
	mod := testMessageModule(t)
	owner, sender := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: sender, Recipient: owner, Content: "x"})

	list := mod.listMessagesTool(owner)

	_, first, err := list(context.Background(), nil, listMessagesIn{})
	if err != nil || first.NextSince == "" {
		t.Fatalf("first listing: next_since %q, err %v", first.NextSince, err)
	}

	_, again, err := list(context.Background(), nil, listMessagesIn{Since: first.NextSince})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 0 {
		t.Fatalf("the cursor answered %v old rows", len(again.Messages))
	}
	if again.NextSince != first.NextSince {
		t.Fatalf("cursor: got %q, want the one sent, %q", again.NextSince, first.NextSince)
	}
}
