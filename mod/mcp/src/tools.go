package mcp

import (
	"encoding/base64"
	"encoding/json"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools this module registers itself. They are the agent's floor, and a
// declared tool may not take one of these names — a configuration that
// overrode one would silently repoint it.
const (
	toolQuery  = "astral-query"
	toolWhoami = "astral-whoami"

	toolSendMessage = "send_message"
	toolInbox       = "inbox"
	toolReadMessage = "read_message"
	toolReadNext    = "read_next"
	toolOutbox      = "outbox"
)

var builtinTools = []string{
	toolQuery, toolWhoami,
	toolSendMessage, toolInbox, toolReadMessage, toolReadNext, toolOutbox,
}

// addTools registers the astral tool set on an MCP server. Every handler is
// bound to the authenticated agent identity by closure.
func (mod *Module) addTools(s *mcpsdk.Server, agentID *astral.Identity) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolQuery,
		Description: "Send a query to a node service on the astral network. " +
			"Services answer with framed objects and the response format is " +
			"auto-detected, so the default works. This reaches no agent: an " +
			"agent answers no query but the one that delivers a message, so " +
			"write to another agent with send_message.",
	}, mod.queryTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        toolWhoami,
		Description: "Get your agent identity plus the host node and its user.",
	}, mod.whoamiTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolSendMessage,
		Description: "Send a message to another agent. It lands in that " +
			"agent's inbox and waits there until read, so the recipient need " +
			"not be running. Returns the message id. To answer a message you " +
			"received, send one back to its sender.",
	}, mod.sendMessageTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolInbox,
		Description: "List the messages waiting for you: sender, arrival and " +
			"read state, without their bodies. Read one with read_message.",
	}, mod.inboxTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        toolReadMessage,
		Description: "Read one message by id and mark it read.",
	}, mod.readMessageTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolReadNext,
		Description: "Wait for your oldest unread message, claim it and " +
			"return it, or {status: timeout} when none arrived. Nothing is " +
			"lost between calls: a message that arrives while you are not " +
			"reading waits in the inbox. Each call takes at most one message: " +
			"report what happened rather than looping indefinitely, unless " +
			"asked to keep serving.",
	}, mod.readNextTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolOutbox,
		Description: "List what you sent and what became of each one: the " +
			"recipient, when it was sent, whether their node stored it, " +
			"whether it failed, and whether the body was handed out. A row " +
			"with only a sent time is a send whose outcome is unknown. Pass " +
			"an id to ask about one send, or awaiting_pickup to see only the " +
			"ones sitting unread in a recipient's mailbox. Handed out means " +
			"their node gave the message to the reader, not that a model " +
			"considered it, so an answer that matters is still the thing to " +
			"wait for. A recipient on another node reports the pickup once " +
			"and nothing repeats it, so a missing handed-out time is not " +
			"proof the message was never taken.",
	}, mod.outboxTool(agentID))

	// The deployment's own tools, registered after the set above so a name it
	// cannot take is one this module already holds — see readDeclaredTools.
	for _, tool := range mod.tools {
		mcpsdk.AddTool(s, &mcpsdk.Tool{
			Name:        tool.name,
			Description: tool.description,
		}, mod.declaredToolHandler(agentID, tool))
	}
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
