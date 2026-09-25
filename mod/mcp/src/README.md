# mcp

mcp serves the Model Context Protocol over streamable HTTP, so an AI agent can
exchange messages with other agents and call the tools its deployment declares.
An agent is a [messaging](../../messaging/src/README.md) participant this module
records as an agent. An agent authenticates with its access token as a bearer
token, and every tool acts as that identity. The mail tools call mod/messaging
directly; mod/messaging hosts the mailbox and carries the mail.

An agent is served exactly five built-in tools — `send_message`,
`list_messages`, `read_messages`, `wait` and `archive` — and the tools the
deployment declares. No built-in tool puts a query.

## Configuration

The config file for the module is `mcp.yaml`.

### Endpoint

The endpoint the MCP server listens on. Empty disables the server:

```yaml
bind_mcp: "tcp:127.0.0.1:8626"
```

### Agents

`mcp.create_agent` mints the participant through mod/messaging, records the
agent, and answers its access token. The token's validity comes from the op's
`duration` argument, or from `token_duration` in `messaging.yaml`.

### Queries

Every declared tool is single-shot, bounded by:

```yaml
query_timeout: 15s
max_response_bytes: 65536
max_response_objects: 64
```

The mail tools are bounded by mod/messaging: `max_payload_bytes` bounds a
message body, and `max_read_bytes` the bodies one read answers.

### Waiting

The `wait` tool parks until something arrives in the agent's inbox. The window
is granted by mod/messaging from `wait_default` and `wait_max` in
`messaging.yaml`, and every answer names `granted_secs` beside `waited_secs`.

Both bounds are the deployment's because what caps a held call is the MCP
client's own request timeout and any proxy in front of it, neither of which the
node can see. The defaults sit under the untuned request caps of the surveyed
clients and under the endpoint's thirty-minute session timeout. A deployment
behind a sixty-second proxy names its own.

A caller that sends a `progressToken` is answered a `notifications/progress`
every ten seconds the park is held, carrying the seconds spent as `progress` and
the granted window as `total`. A caller that sends no token is answered none: a
notification may name only a token from an active request. Whether the
notification lifts the client's own timeout is the client's to decide.

### Declared tools

A deployment can expose an astral query to every agent as a named tool. The
description is what the agent's model reads to decide whether to call it, and it
is configuration because what the answer means belongs to the answering service:

```yaml
tools:
  - name: contacts_list
    description: List the contacts this node holds.
    query: "astral://contacts:contacts.list"
```

The query is `astral://<identity-or-alias>:<query>`. A tool may not take the
name of a built-in — `send_message`, `list_messages`, `read_messages`, `wait`,
`archive` — and a duplicate or shadowed name fails the load rather than
silently repointing a name the agent already knows.

A declared tool asks the node no authorization action. Its query names the
agent as the caller and carries the MCP origin, so the node's own operations
refuse it. A target on this node reads the agent as the caller.

The query is routed on the node's context, as a delivery is in
[messaging](../../messaging/src/README.md). A link carries it as a relay query
naming the agent and the target. The far node admits it only under the agent's
relay contract, and then reads the agent as the caller. A query routed on a
context naming the agent would cross the link as a plain query, and the far
node would answer it with this node's authority.

A tool whose target does not resolve answers `unknown target: <target>`. A
query that is refused or finds no route answers `query failed: <error>`.
