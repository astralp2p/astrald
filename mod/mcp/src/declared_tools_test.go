package mcp

import (
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
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
			configs: []ToolConfig{{Name: toolQuery, Description: "foo", Query: valid.Query}},
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
// router mounting every module's operations refuses — mod/shell/src. A tool a
// deployment points at a node operation therefore fails rather than reaching
// one, and this is the call site that has to keep carrying it.
func TestADeclaredToolPutsAnMcpQuery(t *testing.T) {
	q := declaredQuery(astral.GenerateIdentity(), astral.GenerateIdentity(), "mcp.list_agents")

	if !q.IsMCP() {
		t.Fatal("the query does not carry the MCP origin")
	}
	if q.IsLocal() {
		t.Fatal("the query reads as local, which is what a node operation admits")
	}
}
