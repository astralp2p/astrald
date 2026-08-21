package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
)

const ModuleName = "mcp"
const DBPrefix = "mcp__"

// Module is the public API surface of the mcp module.
//
// The wire types the module answers with — mcp.Agent and mcp.AgentInfo — are
// declared by astral-go's api/mcp and registered there, as api/apphost declares
// apphost.AccessToken. A type declared in both places would be registered twice
// under one object type, and astral.Add refuses the second.
type Module interface {
	Agents() ([]*mcp.Agent, error)
}
