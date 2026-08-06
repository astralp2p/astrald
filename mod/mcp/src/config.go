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

	// ListenTimeout caps how long astral-listen parks waiting for a query.
	ListenTimeout time.Duration `yaml:"listen_timeout,omitempty"`

	// SessionTTL is the idle timeout of a dialog session, refreshed on every
	// send and receive.
	SessionTTL time.Duration `yaml:"session_ttl,omitempty"`

	// PendingTTL is how long an inbound query for a registered agent waits in
	// the pending queue for the agent's next astral-listen call.
	PendingTTL time.Duration `yaml:"pending_ttl,omitempty"`

	// MaxPending caps the queued inbound queries per agent.
	MaxPending int `yaml:"max_pending,omitempty"`

	// PayloadReadWindow is how long astral-listen waits for request bytes
	// after accepting an inbound query.
	PayloadReadWindow time.Duration `yaml:"payload_read_window,omitempty"`

	MaxResponseBytes   int `yaml:"max_response_bytes,omitempty"`
	MaxResponseObjects int `yaml:"max_response_objects,omitempty"`
	MaxPayloadBytes    int `yaml:"max_payload_bytes,omitempty"`
}

var defaultConfig = Config{
	BindMCP:       "tcp:127.0.0.1:8626",
	TokenDuration: 365 * 24 * time.Hour,
	QueryTimeout:  15 * time.Second,
	// why: common MCP clients cap tool calls near 60s; stay under it so a
	// quiet listen returns a clean timeout result instead of a client error.
	ListenTimeout: 55 * time.Second,
	SessionTTL:    5 * time.Minute,
	// why: agents call astral-listen in gaps while their model thinks; queuing
	// the query means the caller waits instead of failing, and the agent need
	// not be parked at the exact moment the query lands.
	PendingTTL:         30 * time.Second,
	MaxPending:         8,
	PayloadReadWindow:  time.Second,
	MaxResponseBytes:   64 << 10,
	MaxResponseObjects: 64,
	MaxPayloadBytes:    64 << 10,
}
