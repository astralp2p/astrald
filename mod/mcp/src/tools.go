package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var errUnknownSession = errors.New("unknown or expired session")

// addTools registers the astral tool set on an MCP server. Every handler is
// bound to the authenticated agent identity by closure.
func (mod *Module) addTools(s *mcpsdk.Server, agentID *astral.Identity) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: "astral-query",
		Description: "Send a query to an identity on the astral network. " +
			"Node services answer with framed objects, other agents answer with " +
			"plain text — the response format is auto-detected, so the default " +
			"works for both. For a multi-turn dialog set session:true and " +
			"continue with astral-send/astral-receive; another agent may take " +
			"many seconds to answer, so give astral-receive a generous timeout_ms.",
	}, mod.queryTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: "astral-listen",
		Description: "Wait for an incoming query addressed to your identity. " +
			"Returns the caller, query and request payload plus a session_id " +
			"for answering via astral-send and reading more via astral-receive. " +
			"On {status: timeout} simply call it again to keep listening, and " +
			"call it again promptly after handling a query — callers are only " +
			"held for a few seconds while you are between calls.",
	}, mod.listenTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: "astral-send",
		Description: "Write data to an open session. Set close to end the " +
			"session after writing; close with empty data just closes it.",
	}, mod.sendTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: "astral-receive",
		Description: "Read the next data from an open session. Returns " +
			"{status: timeout} when nothing arrived and {status: closed} when " +
			"the remote side ended the session. When talking to another agent " +
			"give it a generous timeout_ms — their model needs time to answer.",
	}, mod.receiveTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "astral-whoami",
		Description: "Get your agent identity plus the host node and its user.",
	}, mod.whoamiTool(agentID))
}

// agentSession returns the session only if it belongs to the agent.
func (mod *Module) agentSession(agentID *astral.Identity, id string) (*session, error) {
	s, ok := mod.sessions.Get(id)
	if !ok || s.agent != agentID.String() {
		return nil, errUnknownSession
	}
	return s, nil
}

// jsonValue re-parses a JSON document for schema-checked tool output.
func jsonValue(doc []byte) (v any) {
	_ = json.Unmarshal(doc, &v)
	return
}

// decodePayload converts tool-call data into wire bytes.
func decodePayload(data string, isBase64 bool) ([]byte, error) {
	if data == "" {
		return nil, nil
	}
	if isBase64 {
		return base64.StdEncoding.DecodeString(data)
	}
	return []byte(data), nil
}

// encodePayload renders wire bytes for a tool result: utf8 text when valid,
// base64 otherwise.
func encodePayload(data []byte) (payload, encoding string) {
	if len(data) == 0 {
		return "", ""
	}
	if utf8.Valid(data) {
		return string(data), "utf8"
	}
	return base64.StdEncoding.EncodeToString(data), "base64"
}
