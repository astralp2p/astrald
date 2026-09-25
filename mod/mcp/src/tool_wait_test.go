package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A timed-out answer keeps the caller's cursor and accounts its window in whole
// seconds; an answered one names the furthest row.
func TestATimedOutAnswerKeepsTheCursor(t *testing.T) {
	mod, msg := testAgentModule(t)
	owner := astral.GenerateIdentity()
	wait := mod.waitTool(owner)

	msg.waited = &messaging.WaitResult{
		Messages:  []*messaging.Envelope{testEnvelope(astral.GenerateIdentity(), owner, 9)},
		NextSince: 9,
		Granted:   astral.Duration(2 * time.Minute),
		Waited:    astral.Duration(3 * time.Millisecond),
	}
	_, first, err := wait(context.Background(), nil, waitIn{})
	if err != nil {
		t.Fatal(err)
	}
	if first.TimedOut || first.NextSince != "9" {
		t.Fatalf("first wait: timed_out %v, next_since %q", first.TimedOut, first.NextSince)
	}
	if first.GrantedSecs != 120 || first.WaitedSecs != 0 {
		t.Fatalf("window: granted %v waited %v, want 120 and 0", first.GrantedSecs, first.WaitedSecs)
	}

	msg.waited = &messaging.WaitResult{
		NextSince: 9,
		TimedOut:  true,
		Granted:   astral.Duration(time.Second),
		Waited:    astral.Duration(time.Second + 2*time.Millisecond),
	}
	_, again, err := wait(context.Background(), nil, waitIn{Since: first.NextSince, TimeoutSecs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if msg.waitReq.Since != 9 || msg.waitReq.Timeout != time.Second {
		t.Fatalf("the park reached messaging as %+v", msg.waitReq)
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

	msg.checkCalledAs(t, owner)
}

// A caller is reported to only where it named a progress token, because a
// notification may name only a token from an active request.
func TestOnlyANamedTokenIsReportedTo(t *testing.T) {
	ctx := context.Background()

	cases := map[string]*mcpsdk.CallToolRequest{
		"no request":     nil,
		"no params":      {},
		"no session":     {Params: &mcpsdk.CallToolParamsRaw{}},
		"no token named": {Params: &mcpsdk.CallToolParamsRaw{}, Session: &mcpsdk.ServerSession{}},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if waitProgress(ctx, req) != nil {
				t.Fatal("a caller that named no progress token is reported to")
			}
		})
	}
}
