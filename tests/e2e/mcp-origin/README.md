# mcp-origin — an agent reaches its peers and no node operations

An agent is served the five mail tools and the tools its deployment declares in
`mcp.yaml`. No built-in tool puts a query. A declared tool puts a fixed query as
the agent, and a deployment may point one at any path. Every astrald operation
sits behind one router: mod/shell mounts each loaded module's op router as a
scope and answers on the node's own identity, so a tool pointed at the node
would reach every one of them, the `messaging.*` ops included.

mod/mcp marks the queries it routes with `astral.OriginMCP`, and mod/shell
refuses that origin before reaching the scopes. Every `messaging.*` op refuses
it again on its own.

Every node that serves MCP declares one tool per node operation in
`lib/nodeconfig.py` `NODE_OP_TOOLS`: `shell.shell`, `mcp.list_agents` and
`messaging.create_identity`, each put to `localnode`. alpha calls each of them
over MCP.

## What this proves that the unit tests do not

The unit tests hold each half — that `launch` stamps, that mod/shell refuses a
stamped query. Neither runs the path between them: a real bearer token, the real
streamable-HTTP listener, a tool read from `mcp.yaml`, `core.Router`, and
whichever router claims the target. This drives that path.

The oracle also requires the agent's tool set to be exactly the five mail tools
and the tools the node declares: the three node-op tools and the
`PEER_TOOLS` that `mcp-peer` calls. A tool that routes whatever query an agent
names fails the test.

## Why the driver serves an authority

A declared tool asks the node no authorization action: its target decides whom
it answers. A message proceeds only when `mod.messaging.send_action` is granted
to its sender and `mod.messaging.receive_action` to its recipient. No handler
grants either, so a node carries no message unless an external authority does.
The harness names one for every local node in `auth.yaml`, on the node's own
loopback port (`authority_url` in `session.json`); an unserved port refuses.

The driver serves it. It admits alpha and beta to each other in both
directions, and nothing else, and it records every question with its answer.
The oracle checks that record first: the refusal of gamma is evidence only if
the authority was asked and refused it, and the exchange is evidence only if
the authority was asked, naming the right actor, and admitted it. The record
also shows that the authority is asked the two mail actions and nothing else.

A node that still asked an authorization action before a declared tool routed
would refuse the tool with `unknown target`, since no handler and no configured
authority grants it. `unknown target` does not count as the refusal under
test, so such a node fails.

## Why the control calls matter

A refusal and a missing operation leave the caller holding the same nothing.
Each node op also refuses a caller without its permit, so alpha is granted
`mod.shell.shell_action` and `mod.auth.admin_manage_apps_action`, and the driver
records `mcp.list_agents` and `messaging.create_identity` answered to alpha over
apphost, on the same node in the same run. The oracle checks them first. Only
the origin differs between those answers and the refusals over MCP.

The oracle also requires each failure to *read* as a refusal. A timeout, a 401
or a dead listener would otherwise keep this green while the guard was gone.

## Why an exchange is in here

A guard that refused everything would satisfy every check above and destroy the
product. alpha mails beta through the MCP tools and beta replies, so the test
fails if the fix takes agent-to-agent with it. The mail tools reach
mod/messaging by direct calls under the bearer's identity, not through a query,
so the origin guard never stands between them.
