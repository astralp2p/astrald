package mcp

const ModuleName = "mcp"
const DBPrefix = "mcp__"

// Module is the public API surface of the mcp module.
type Module interface {
	Agents() ([]*Agent, error)
}
