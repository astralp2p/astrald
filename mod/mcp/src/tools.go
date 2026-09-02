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
	toolQuery = "astral-query"

	toolSendMessage  = "send_message"
	toolListMessages = "list_messages"
	toolReadMessages = "read_messages"
	toolWait         = "wait"
	toolArchive      = "archive"
)

var builtinTools = []string{
	toolQuery,
	toolSendMessage, toolListMessages, toolReadMessages, toolWait, toolArchive,
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
		Name: toolSendMessage,
		Description: "Send a message to another agent. Their node stores it " +
			"and it waits in their inbox until they read it, so the agent " +
			"need not be listening — but their node must answer now, and the " +
			"send fails if it does not. Returns the id it is stored under, " +
			"which is also what a later message names as its parent. To answer a " +
			"message, pass that message's id as parent_id: the other side " +
			"then reads which of its messages you answered rather than " +
			"guessing from who wrote and when.",
	}, mod.sendMessageTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolListMessages,
		Description: "List your messages without their bodies. types is " +
			"inbox (what was written to you, the default), outbox (what you " +
			"wrote) or archive (what you put away, in either direction). " +
			"Narrow an inbox with from and unread_only, an outbox with to and " +
			"awaiting_pickup; a filter that does not apply to the list you " +
			"named is refused rather than ignored. Each row carries the box " +
			"beside the id, and read_messages wants both.",
	}, mod.listMessagesTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolReadMessages,
		Description: "Read whole messages, bodies included, and mark the ones " +
			"in your inbox read. Name each by box and id, as a listing gave " +
			"them. Each message names every direct reply it has in child_ids " +
			"— the messages that name it as their parent, from either box — " +
			"and read_messages will answer any of them. Some of those replies " +
			"come back beside it as well, as envelopes by default, or with " +
			"children: full to read their bodies too. " +
			"An id you do not hold comes back under not_found and the rest " +
			"are still read. Reading is not a claim: reading twice answers " +
			"the same messages unchanged.",
	}, mod.readMessagesTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolWait,
		Description: "Park until something arrives in your inbox that you have " +
			"not put away, and answer it without its body — read it with " +
			"read_messages. Pass from to wait for one correspondent, and " +
			"since (from a previous answer's next_since) to see only what is " +
			"newer. timeout_secs asks for the window in seconds; the " +
			"deployment's ceiling is the most any ask is granted, and every " +
			"answer names granted_secs beside waited_secs. timed_out means " +
			"the granted window closed with nothing new. " +
			"Nothing is claimed and nothing is consumed, so a message you " +
			"leave alone is answered again next time: archive is what says " +
			"you are done with it. Each call parks once — report what " +
			"happened rather than looping indefinitely, unless you were asked " +
			"to keep serving. Nothing is lost between calls.",
	}, mod.waitTool(agentID))

	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name: toolArchive,
		Description: "Put one message away, naming its box and id. It leaves " +
			"the inbox and outbox listings, appears under types: archive, and " +
			"wait never answers it again — so this is what ends a message " +
			"rather than reading it. Pass undo to put it back. changed is " +
			"false when the message was already the way you asked for, and " +
			"when it is not yours. It is your own bookkeeping: the other " +
			"party learns nothing from it.",
	}, mod.archiveTool(agentID))

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
