package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubNode struct {
	id *astral.Identity
}

func (n *stubNode) Identity() *astral.Identity { return n.id }

func (n *stubNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return nil, astral.NewErrRouteNotFound()
}

type bearerTransport struct {
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func testMCPServer(t *testing.T) (*httptest.Server, *astral.Identity) {
	t.Helper()

	agentID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()

	mod := &Module{
		ctx:    astral.NewContext(nil),
		node:   &stubNode{id: nodeID},
		config: defaultConfig,
	}
	mod.Apphost = &stubApphost{tokens: map[string]*astral.Identity{"good-token": agentID}}
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{
		"my-agent": agentID,
		"my-node":  nodeID,
	}}

	ts := httptest.NewServer(NewMCPServer(mod).handler())
	t.Cleanup(ts.Close)

	return ts, agentID
}

func TestMCPServerRejectsBadToken(t *testing.T) {
	ts, _ := testMCPServer(t)

	req, _ := http.NewRequest("POST", ts.URL, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer bad-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %v, want 401", resp.StatusCode)
	}
}

func TestMCPServerRejectsMissingToken(t *testing.T) {
	ts, _ := testMCPServer(t)

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %v, want 401", resp.StatusCode)
	}
}

func TestMCPServerWhoami(t *testing.T) {
	ts, agentID := testMCPServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: &bearerTransport{token: "good-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("%v tools listed, want 5", len(tools.Tools))
	}

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "astral-whoami"})
	if err != nil {
		t.Fatalf("call whoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("whoami returned tool error: %v", res.Content)
	}

	var out whoamiOut
	if err = json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("parse whoami: %v", err)
	}
	if out.AgentID != agentID.String() {
		t.Fatalf("agent_id %v, want %v", out.AgentID, agentID)
	}
	if out.AgentAlias != "my-agent" || out.NodeAlias != "my-node" {
		t.Fatalf("aliases %v/%v, want my-agent/my-node", out.AgentAlias, out.NodeAlias)
	}
	if out.UserID != "" {
		t.Fatalf("user_id %v on a module without user dep", out.UserID)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
