package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
)

const ModuleName = "mcp"
const DBPrefix = "mcp__"

// Module is the public API surface of the mcp module.
//
// The types the module works in — mcp.Agent and mcp.AgentInfo for a record —
// are declared by astral-go's api/mcp and registered there, as api/apphost
// declares apphost.AccessToken. A type declared in both places would be
// registered twice under one object type, and astral.Add refuses the second.
//
// An agent is a messaging participant. Its mail is the messaging module's, and
// the MCP tool structs render what that module answers into the schema the
// endpoint declares.
type Module interface {
	Agents() ([]*mcp.Agent, error)
}
