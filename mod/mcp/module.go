package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
)

const ModuleName = "mcp"
const DBPrefix = "mcp__"

// Module is the public API surface of the mcp module.
//
// The types the module works in — mcp.Agent and mcp.AgentInfo for a record,
// mcp.Message for a delivery and mcp.StoredMessage for what a node holds
// afterwards — are declared by astral-go's api/mcp and registered there, as
// api/apphost declares apphost.AccessToken. A type declared in both places
// would be registered twice under one object type, and astral.Add refuses the
// second.
//
// The store's own row types stay in src and never leave it: a mailbox answers
// mcp.StoredMessage, and the MCP tool structs render that into the schema the
// endpoint declares.
type Module interface {
	Agents() ([]*mcp.Agent, error)
}
