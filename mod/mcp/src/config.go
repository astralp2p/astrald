package mcp

import "time"

type Config struct {
	// BindMCP is the endpoint the MCP server listens on; empty disables it.
	BindMCP string `yaml:"bind_mcp,flow"`

	// QueryTimeout bounds the response window of a declared tool's query.
	QueryTimeout time.Duration `yaml:"query_timeout,omitempty"`

	MaxResponseBytes   int `yaml:"max_response_bytes,omitempty"`
	MaxResponseObjects int `yaml:"max_response_objects,omitempty"`

	// Tools are the tools this deployment exposes beside the built-in set.
	Tools []ToolConfig `yaml:"tools,omitempty"`
}

// ToolConfig is one tool a deployment exposes to every agent.
//
// why the description is configuration: what the tool means is the answering
// service's, and this module never reads the answer. The deployment that knows
// what the query returns is the one that describes it.
type ToolConfig struct {
	// Name is the tool as the agent calls it. It may not be a built-in name.
	Name string `yaml:"name"`

	// Description is what the agent's model reads to decide whether to call it.
	Description string `yaml:"description"`

	// Query is what the tool runs, as astral://<identity-or-alias>:<query>.
	Query string `yaml:"query"`
}

var defaultConfig = Config{
	BindMCP:            "tcp:127.0.0.1:8626",
	QueryTimeout:       15 * time.Second,
	MaxResponseBytes:   64 << 10,
	MaxResponseObjects: 64,
}
