package mcp

import "time"

// AgentContractDuration is the validity of the node→agent relay contract,
// mirroring apphost's RegisterDuration.
const AgentContractDuration = 10 * 365 * 24 * time.Hour

type Config struct {
	// BindMCP is the endpoint the MCP server listens on; empty disables it.
	BindMCP string `yaml:"bind_mcp,flow"`

	// TokenDuration is the validity of the access token issued to a new agent.
	TokenDuration time.Duration `yaml:"token_duration,omitempty"`

	// QueryTimeout bounds the response window of a single-shot astral-query.
	QueryTimeout time.Duration `yaml:"query_timeout,omitempty"`

	// ReadTimeout caps how long read_next waits for a message.
	ReadTimeout time.Duration `yaml:"read_timeout,omitempty"`

	MaxResponseBytes   int `yaml:"max_response_bytes,omitempty"`
	MaxResponseObjects int `yaml:"max_response_objects,omitempty"`
	MaxPayloadBytes    int `yaml:"max_payload_bytes,omitempty"`

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
	BindMCP:       "tcp:127.0.0.1:8626",
	TokenDuration: 365 * 24 * time.Hour,
	QueryTimeout:  15 * time.Second,
	// why: common MCP clients cap tool calls near 60s; stay under it so a quiet
	// read returns a clean timeout result instead of a client error.
	ReadTimeout:        55 * time.Second,
	MaxResponseBytes:   64 << 10,
	MaxResponseObjects: 64,
	MaxPayloadBytes:    64 << 10,
}
