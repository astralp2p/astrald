package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maps"
	"regexp"
	"slices"

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

// A description that names a field the answer does not carry sends an agent
// looking for a key that is not there, and nothing fails — the call succeeds
// and the model reads a missing value. That is what the archived/changed rename
// did: the field moved and the prose describing it did not.
//
// The check is over what the agent is actually served, not over the source, and
// it is narrow on purpose: a token immediately followed by "is false", "is true"
// or "means" is a description talking about a field's value, and there is no
// other reason to write that.
func TestNoToolDescribesAFieldItDoesNotHave(t *testing.T) {
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

	namesAValue := regexp.MustCompile(`\b([a-z][a-z0-9_]*)\s+(?:is (?:false|true)|means)\b`)

	var checked int
	for _, tool := range tools.Tools {
		fields := schemaFields(tool.InputSchema)
		maps.Copy(fields, schemaFields(tool.OutputSchema))

		for _, m := range namesAValue.FindAllStringSubmatch(tool.Description, -1) {
			checked++
			if !fields[m[1]] {
				t.Errorf("%v describes %q, which is not a field it accepts or answers; it has %v",
					tool.Name, m[1], slices.Sorted(maps.Keys(fields)))
			}
		}
	}

	if checked == 0 {
		t.Fatal("the check matched nothing — the descriptions or the pattern moved")
	}
	t.Logf("%v field references checked across %v tools", checked, len(tools.Tools))
}

// schemaFields answers the property names of a tool schema as it arrives over
// the wire, where it is a decoded JSON object rather than a typed schema.
func schemaFields(schema any) map[string]bool {
	out := map[string]bool{}

	obj, ok := schema.(map[string]any)
	if !ok {
		return out
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return out
	}
	for name := range props {
		out[name] = true
	}
	return out
}
