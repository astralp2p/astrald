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

	// WaitDefault is how long wait parks when the caller names no window, and
	// WaitMax is the most any caller's ask is granted.
	//
	// why the deployment names both: the bounds are not the node's to know.
	// What caps a held call is whatever sits between the agent and the
	// endpoint — the MCP client's own request timeout, and any proxy in front
	// — and the deployment is what knows the chain. They are two knobs because
	// the jobs differ: the default serves the caller that named nothing, and
	// the ceiling bounds the one that asked for more than the chain survives.
	WaitDefault time.Duration `yaml:"wait_default,omitempty"`
	WaitMax     time.Duration `yaml:"wait_max,omitempty"`

	// WaitTimeout is the retired name for the one knob that was both of the
	// above. A deployment still naming it keeps the old meaning: the value
	// serves as the default and as the ceiling alike, wherever the new names
	// are absent.
	WaitTimeout time.Duration `yaml:"wait_timeout,omitempty"`

	MaxResponseBytes   int `yaml:"max_response_bytes,omitempty"`
	MaxResponseObjects int `yaml:"max_response_objects,omitempty"`
	MaxPayloadBytes    int `yaml:"max_payload_bytes,omitempty"`

	// Tools are the tools this deployment exposes beside the built-in set.
	Tools []ToolConfig `yaml:"tools,omitempty"`
}

// waitDefault answers the window an ask that names none parks for.
//
// why two minutes: no cap comes from the protocol and the SDK imposes no
// server-side deadline, so the bound that matters is the client's own.
// Surveyed hosts' untuned caps sit at five idle minutes, five flat, and ten
// (Claude Code, Codex, Gemini; 2026-09-02) — two minutes parks under every
// one of them. A deployment serving a sixty-second client names its own.
func (c Config) waitDefault() time.Duration {
	switch {
	case c.WaitDefault > 0:
		return c.WaitDefault
	case c.WaitTimeout > 0:
		return c.WaitTimeout
	}
	return 2 * time.Minute
}

// waitMax answers the most any caller's ask is granted.
//
// why fifteen minutes: the park must end before the session that carries it —
// the endpoint's session timeout is thirty minutes — and a longer grant buys
// only fewer round-trips, because re-parking takes nothing and the cursor
// loses nothing.
func (c Config) waitMax() time.Duration {
	switch {
	case c.WaitMax > 0:
		return c.WaitMax
	case c.WaitTimeout > 0:
		return c.WaitTimeout
	}
	return 15 * time.Minute
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
	TokenDuration:      365 * 24 * time.Hour,
	QueryTimeout:       15 * time.Second,
	MaxResponseBytes:   64 << 10,
	MaxResponseObjects: 64,
	MaxPayloadBytes:    64 << 10,
}
