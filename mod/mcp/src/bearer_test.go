package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every mail tool acts as the identity its bearer token authenticates, and a
// session under another token on the same server acts as that token's
// identity: no tool argument names a participant, so the token alone chooses
// the mailbox messaging acts on.
func TestEveryMailToolActsAsTheBearer(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	msg := &fakeMessaging{}
	ts := testMailServer(t, msg, map[string]*astral.Identity{"token-a": a, "token-b": b})

	callEveryMailTool(t, ts.URL, "token-a")
	callEveryMailTool(t, ts.URL, "token-b")

	msg.mu.Lock()
	defer msg.mu.Unlock()

	n := len(mailToolCalls())
	if len(msg.callers) != 2*n {
		t.Fatalf("messaging was called %v times, want once per mail tool per session", len(msg.callers))
	}
	for i, caller := range msg.callers {
		if want := []*astral.Identity{a, b}[i/n]; !caller.IsEqual(want) {
			t.Fatalf("call %v was made as %v, want the session's bearer %v", i, caller, want)
		}
	}
}

// testMailServer serves MCP over the fake messaging module, authenticating the
// tokens given.
func testMailServer(t *testing.T, msg *fakeMessaging, tokens map[string]*astral.Identity) *httptest.Server {
	t.Helper()

	mod := &Module{
		ctx:    astral.NewContext(nil),
		node:   &stubNode{id: astral.GenerateIdentity()},
		config: defaultConfig,
	}
	mod.Apphost = &stubApphost{tokens: tokens}
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{}}
	mod.Messaging = msg

	ts := httptest.NewServer(NewMCPServer(mod).handler())
	t.Cleanup(ts.Close)

	return ts
}

// mailToolCalls names each mail tool with arguments it accepts.
func mailToolCalls() map[string]map[string]any {
	id := messaging.NewMessageID().String()
	ref := map[string]any{"box": messaging.BoxInbox, "id": id}

	return map[string]map[string]any{
		toolSendMessage:  {"to": "peer", "content": "x"},
		toolListMessages: {},
		toolReadMessages: {"ids": []any{ref}},
		toolWait:         {"timeout_secs": 1},
		toolArchive:      ref,
	}
}

// callEveryMailTool calls each mail tool once over MCP under the bearer token,
// and asserts none answered an error.
func callEveryMailTool(t *testing.T, endpoint, token string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: &bearerTransport{token: token}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	for name, args := range mailToolCalls() {
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%v: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%v answered an error: %v", name, res.Content)
		}
	}
}
