package mcp

import (
	"context"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type whoamiIn struct{}

type whoamiOut struct {
	AgentID    string `json:"agent_id" jsonschema:"your identity on the astral network"`
	AgentAlias string `json:"agent_alias,omitempty" jsonschema:"your alias"`
	NodeID     string `json:"node_id" jsonschema:"the host node's identity"`
	NodeAlias  string `json:"node_alias,omitempty" jsonschema:"the host node's alias"`
	UserID     string `json:"user_id,omitempty" jsonschema:"the node owner's identity, absent on an unclaimed node"`
	UserAlias  string `json:"user_alias,omitempty" jsonschema:"the node owner's alias"`
}

func (mod *Module) whoamiTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[whoamiIn, whoamiOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ whoamiIn) (res *mcpsdk.CallToolResult, out whoamiOut, err error) {
		nodeID := mod.node.Identity()

		out.AgentID = agentID.String()
		out.AgentAlias, _ = mod.Dir.GetAlias(agentID)
		out.NodeID = nodeID.String()
		out.NodeAlias, _ = mod.Dir.GetAlias(nodeID)

		if mod.User != nil {
			if userID := mod.User.Identity(); userID != nil {
				out.UserID = userID.String()
				out.UserAlias, _ = mod.Dir.GetAlias(userID)
			}
		}

		return nil, out, nil
	}
}
