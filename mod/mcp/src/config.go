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

	// WaitDefault is the window a wait that names none parks for, and WaitMax
	// the most any ask is granted.
	//
	// why the deployment names both: what caps a held call is the MCP client's
	// own request timeout and any proxy in front of it, which the node cannot
	// know. The default serves a caller that named nothing; the ceiling bounds
	// one that asked for more than the chain survives.
	WaitDefault time.Duration `yaml:"wait_default,omitempty"`
	WaitMax     time.Duration `yaml:"wait_max,omitempty"`

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
	// why two minutes and fifteen: surveyed hosts' untuned client caps sit at
	// five idle minutes, five flat and ten (Claude Code, Codex, Gemini;
	// 2026-09-02), and the endpoint's session times out at thirty.
	WaitDefault:        2 * time.Minute,
	WaitMax:            15 * time.Minute,
	MaxResponseBytes:   64 << 10,
	MaxResponseObjects: 64,
	MaxPayloadBytes:    64 << 10,
}
