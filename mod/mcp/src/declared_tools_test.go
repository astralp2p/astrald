package mcp

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

func TestReadDeclaredTools(t *testing.T) {
	tools, err := readDeclaredTools([]ToolConfig{{
		Name:        "list-agents",
		Description: "List the other agents belonging to your owner.",
		Query:       "astral://telepathy:agents.siblings",
	}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("read %d tools, want 1", len(tools))
	}

	tool := tools[0]
	if tool.name != "list-agents" || tool.target != "telepathy" || tool.path != "agents.siblings" {
		t.Fatalf("read %+v", tool)
	}
	if tool.description == "" {
		t.Fatal("the description the agent reads was dropped")
	}
}

// A configuration a start should not survive. Every one of these is a tool the
// agent would never see, or would see pointing somewhere else.
func TestReadDeclaredToolsRefuses(t *testing.T) {
	valid := ToolConfig{
		Name:        "list-agents",
		Description: "List the other agents belonging to your owner.",
		Query:       "astral://telepathy:agents.siblings",
	}

	for _, tc := range []struct {
		name    string
		configs []ToolConfig
		says    string
	}{
		{
			name:    "no name",
			configs: []ToolConfig{{Description: "foo", Query: valid.Query}},
			says:    "no name",
		},
		{
			name:    "no description",
			configs: []ToolConfig{{Name: "list-agents", Query: valid.Query}},
			says:    "no description",
		},
		{
			name:    "two tools of one name",
			configs: []ToolConfig{valid, valid},
			says:    "already a tool",
		},
		{
			name:    "a name this module holds",
			configs: []ToolConfig{{Name: toolSendMessage, Description: "foo", Query: valid.Query}},
			says:    "already a tool",
		},
		{
			name:    "no transport",
			configs: []ToolConfig{{Name: "list-agents", Description: "foo", Query: "telepathy:agents.siblings"}},
			says:    "names no transport",
		},
		{
			name:    "no target",
			configs: []ToolConfig{{Name: "list-agents", Description: "foo", Query: "astral://agents.siblings"}},
			says:    "names no target",
		},
		{
			name:    "no query",
			configs: []ToolConfig{{Name: "list-agents", Description: "foo", Query: "astral://telepathy:"}},
			says:    "names no query",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readDeclaredTools(tc.configs)
			if err == nil {
				t.Fatal("the configuration was read")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("refused with %v, want it to name %q", err, tc.says)
			}
		})
	}
}

// A deployment that declares nothing gets the built-in set and no more.
func TestNoDeclaredToolsIsNoTools(t *testing.T) {
	tools, err := readDeclaredTools(nil)
	if err != nil || len(tools) != 0 {
		t.Fatalf("read %d tools, %v", len(tools), err)
	}
}

// The query a declared tool puts carries the MCP origin, which is what the
// router mounting every module's operations refuses — mod/shell/src — and what
// every messaging operation refuses again. A tool a deployment points at a node
// operation therefore fails rather than reaching one, and this is the call site
// that has to keep carrying it.
func TestADeclaredToolPutsAnMcpQuery(t *testing.T) {
	q := declaredQuery(astral.GenerateIdentity(), astral.GenerateIdentity(), "messaging.send_message")

	if !q.IsMCP() {
		t.Fatal("the query does not carry the MCP origin")
	}
	if q.IsLocal() {
		t.Fatal("the query reads as local, which is what a node operation admits")
	}
}

// answeringNode takes every query routed to it, keeps the last one and the
// identity its routing context named, and answers it one framed object.
type answeringNode struct {
	stubNode
	took   *astral.InFlightQuery
	tookAs *astral.Identity
}

func (n *answeringNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.took = q
	n.tookAs = ctx.Identity()
	go func() {
		sender := channel.NewSender(w)
		sender.Send(&astral.Ack{})
		sender.Send(&astral.EOS{})
		w.Close()
	}()
	return newRecordingWriter(), nil
}

// A declared tool asks the node no authorization action: the target decides
// whom it answers. The authority here refuses everything, so a tool that still
// asked would answer nothing. The query goes out as the agent, carrying the MCP
// origin, and its answer renders as any other.
func TestADeclaredToolAsksNoAction(t *testing.T) {
	agentID, targetID := astral.GenerateIdentity(), astral.GenerateIdentity()
	node := &answeringNode{}
	authority := &recordingAuth{verdict: false}

	mod := testQueryModule(t)
	mod.node = node
	mod.Auth = authority
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{"service": targetID}}

	tool := declaredTool{name: "service-op", description: "foo", target: "service", path: "service.op"}
	_, out, err := mod.declaredToolHandler(agentID, tool)(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("the tool failed: %v", err)
	}

	if asked := authority.recorded(); len(asked) != 0 {
		t.Fatalf("the tool asked the node %v", asked)
	}
	if len(out.Objects) != 1 || out.Payload != "" {
		t.Fatalf("answered %v objects and payload %q, want the one object", len(out.Objects), out.Payload)
	}

	q := node.took
	switch {
	case q == nil:
		t.Fatal("the tool routed no query")
	case !q.Caller.IsEqual(agentID):
		t.Fatalf("the query was put as %v, not the agent", q.Caller)
	case !q.Target.IsEqual(targetID):
		t.Fatalf("the query went to %v, not the tool's target", q.Target)
	case string(q.QueryString) != "service.op":
		t.Fatalf("the query asked %q, not the tool's query", q.QueryString)
	case !q.IsMCP():
		t.Fatal("the query does not carry the MCP origin")
	}
}

// A declared tool's query is routed on the node's context, with the agent as
// its caller. mod/nodes carries a query whose caller differs from the routing
// context's identity over a link as a relay query naming the caller, and one
// whose caller is the context's identity as a plain query the far node reads as
// this node's — mod/nodes/src/mux.go. A context naming the agent would hand a
// peer node's operations this node's authority.
func TestADeclaredToolRoutesAsTheNode(t *testing.T) {
	nodeID, agentID, targetID := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	node := &answeringNode{}

	mod := testQueryModule(t)
	mod.ctx = astral.NewContext(nil).WithIdentity(nodeID).IncludeZone(astral.ZoneNetwork)
	mod.node = node
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{"peer": targetID}}

	tool := declaredTool{name: "peer-op", description: "foo", target: "peer", path: "nodes.links"}
	if _, _, err := mod.declaredToolHandler(agentID, tool)(context.Background(), nil, struct{}{}); err != nil {
		t.Fatalf("the tool failed: %v", err)
	}

	switch {
	case node.took == nil:
		t.Fatal("the tool routed no query")
	case !node.took.Caller.IsEqual(agentID):
		t.Fatalf("the query was put as %v, not the agent", node.took.Caller)
	case !node.tookAs.IsEqual(nodeID):
		t.Fatalf("the query was routed on a context naming %v, not the node — a link carries it as a plain query from this node", node.tookAs)
	}
}
