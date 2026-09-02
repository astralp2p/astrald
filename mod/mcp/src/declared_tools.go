package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/lib/arl"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const astralScheme = "astral://"

// declaredTool is one tool a deployment exposes: a name and a description the
// agent reads, and the query the tool puts on its behalf.
type declaredTool struct {
	name        string
	description string

	// target is the identity as configured, a public key or an alias, and path
	// the query it answers.
	//
	// why the target is not resolved here: tools are read at load and Dir is
	// injected after, so a name resolves when the first agent calls the tool.
	target string
	path   string
}

// readDeclaredTools reads the configured tools, refusing a duplicate name or a
// shadowed built-in: either silently repoints a name the agent already knows.
func readDeclaredTools(configs []ToolConfig) ([]declaredTool, error) {
	taken := make(map[string]bool, len(builtinTools)+len(configs))
	for _, name := range builtinTools {
		taken[name] = true
	}

	tools := make([]declaredTool, 0, len(configs))
	for _, config := range configs {
		switch {
		case config.Name == "":
			return nil, errors.New("declared tool: no name")
		case taken[config.Name]:
			return nil, fmt.Errorf("declared tool %v: the name is already a tool", config.Name)
		case config.Description == "":
			return nil, fmt.Errorf("declared tool %v: no description", config.Name)
		}
		taken[config.Name] = true

		target, path, err := splitEndpoint(config.Query)
		if err != nil {
			return nil, fmt.Errorf("declared tool %v: %w", config.Name, err)
		}

		tools = append(tools, declaredTool{
			name:        config.Name,
			description: config.Description,
			target:      target,
			path:        path,
		})
	}

	return tools, nil
}

// splitEndpoint parses astral://<identity-or-alias>:<query>.
func splitEndpoint(endpoint string) (target, path string, err error) {
	rest, found := strings.CutPrefix(endpoint, astralScheme)
	if !found {
		return "", "", fmt.Errorf("%v names no transport: expected %v", endpoint, astralScheme)
	}

	_, target, path = arl.Split(rest)

	switch {
	case target == "":
		return "", "", fmt.Errorf("%v names no target", endpoint)
	case path == "":
		return "", "", fmt.Errorf("%v names no query", endpoint)
	}

	return target, path, nil
}

// declaredToolHandler puts the tool's query to the tool's target and answers
// what came back. It reads none of it: what the answer means is the answering
// service's, and this module carries bytes.
//
// why the query is the agent's and not the node's: the target decides whether
// this agent may reach it, and a query put as the node would have it decide
// about the wrong identity.
func (mod *Module) declaredToolHandler(agentID *astral.Identity, tool declaredTool) mcpsdk.ToolHandlerFor[struct{}, queryOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (res *mcpsdk.CallToolResult, out queryOut, err error) {
		targetID, err := mod.Dir.ResolveIdentity(tool.target)
		if err != nil {
			return nil, out, fmt.Errorf("unknown target: %v", tool.target)
		}

		// The same question astral-query asks, about the same pair. A tool is a
		// named query and buys the agent no reach it did not have.
		if !mod.Auth.Authorize(mod.ctx, &mcp.CallAgentAction{
			Action: auth.NewAction(agentID),
			ToID:   targetID,
		}) {
			return nil, out, fmt.Errorf("unknown target: %v", tool.target)
		}

		qctx, cancel := mod.ctx.WithIdentity(agentID).WithTimeout(mod.config.QueryTimeout)
		defer cancel()

		conn, err := query.RouteInFlight(qctx, mod.node, declaredQuery(agentID, targetID, tool.path))
		if err != nil {
			return nil, out, fmt.Errorf("query failed: %v", err)
		}

		return nil, mod.collectResponse(conn, "", mod.config.QueryTimeout), nil
	}
}

// declaredQuery is the query a declared tool puts, built apart from the handler
// so that what it carries is testable.
//
// why launch: it stamps the MCP origin the node's own op router refuses, so a
// tool pointed at a node operation fails rather than reaching one.
func declaredQuery(agentID, targetID *astral.Identity, path string) *astral.InFlightQuery {
	return launch(query.New(agentID, targetID, path, nil))
}
