package mcp

import (
	"context"
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

// TestMCPServerListsBuiltins asserts the tool set an authenticated agent is
// served: the six this module registers and nothing else. A deployment's own
// tools are added on top of these — see readDeclaredTools.
func TestMCPServerListsBuiltins(t *testing.T) {
	ts, _ := testMCPServer(t)

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

	listed := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		listed[tool.Name] = true
	}
	if len(listed) != len(builtinTools) {
		t.Fatalf("%v tools listed, want %v", len(listed), len(builtinTools))
	}
	for _, name := range builtinTools {
		if !listed[name] {
			t.Errorf("%v is not listed", name)
		}
	}
	// The tool that answered an agent its own identity is gone: the alias it
	// carried is the node's, and a node holds none for a deployment's agent.
	if listed["astral-whoami"] {
		t.Error("astral-whoami is still served")
	}
}
