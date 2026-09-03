# mcp

mcp serves the Model Context Protocol over streamable HTTP, so an AI agent can
put queries to the astral network and exchange messages with other agents. An
agent is an identity this node mints and holds a mailbox for; it authenticates
with its access token as a bearer token, and every tool acts as that identity.

## Configuration

The config file for the module is `mcp.yaml`.

### Endpoint

The endpoint the MCP server listens on. Empty disables the server:

```yaml
bind_mcp: "tcp:127.0.0.1:8626"
```

### Agents

`mcp.create_agent` mints an agent and answers its access token. The token's
validity comes from the op's `duration` argument, or from:

```yaml
token_duration: 8760h
```

### Queries

`astral-query` and every declared tool are single-shot, bounded by:

```yaml
query_timeout: 15s
max_response_bytes: 65536
max_response_objects: 64
max_payload_bytes: 65536
```

`max_payload_bytes` also bounds a message body, on the way out and on the way
in.

### Waiting

The `wait` tool parks until something arrives in the agent's inbox. A call that
names no window is granted `wait_default`; one that asks for more than
`wait_max` is granted `wait_max`, and every answer names `granted_secs` beside
`waited_secs`:

```yaml
wait_default: 2m
wait_max: 15m
```

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
name of a built-in — `astral-query`, `send_message`, `list_messages`,
`read_messages`, `wait`, `archive` — and a duplicate or shadowed name fails the
load rather than silently repointing a name the agent already knows.

A declared tool buys the agent no reach it did not have: the query is put as the
agent, and the target's authority decides.
