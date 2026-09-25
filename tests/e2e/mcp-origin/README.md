# mcp-origin — an agent reaches its peers and no node operations

`astral-query` takes a target and a path from the model and routes what it
names. Every astrald operation sits behind one router: mod/shell mounts each
loaded module's op router as a scope and answers on the node's own identity, so
an agent permitted to query the node can call every one of them, the
`messaging.*` ops included.

mod/mcp marks the queries it routes with `astral.OriginMCP`, and mod/shell
refuses that origin before reaching the scopes. Every `messaging.*` op refuses
it again on its own.

## What this proves that the unit tests do not

The unit tests hold each half — that `launch` stamps, that mod/shell refuses a
stamped query. Neither runs the path between them: a real bearer token, the real
streamable-HTTP listener, `core.Router`, and whichever router claims the target.
This drives that path.

## Why the driver serves an authority

Neither mod/mcp nor mod/messaging holds reachability of its own.
`astral-query` proceeds only when `mod.mcp.call_agent_action` is granted to the
calling agent. A message proceeds only when `mod.messaging.send_action` is
granted to its sender and `mod.messaging.receive_action` to its recipient. No
handler grants any of the three, so a node answers no query between agents and
carries no message unless an external authority does. The harness names one
for every local node in `auth.yaml`, on the node's own loopback port
(`authority_url` in `session.json`); an unserved port refuses.

The driver serves it. It admits alpha to the node, alpha and beta to each other
in both directions, and nothing else, and it records every question with its
answer. The oracle checks that record first: the refusal of gamma is evidence
only if the authority was asked and refused it, and the exchange is evidence
only if the authority was asked, naming the right actor, and admitted it. The
record also shows that `mod.mcp.call_agent_action` is asked of `astral-query`
alone and never of mail.

`astral-query` asks `mod.mcp.call_agent_action` before it routes anything. An
agent the authority keeps from the node is refused there with `unknown target`
and never reaches mod/shell, so alpha is admitted to the node, and `unknown
target` does not count as the refusal under test.

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
fails if the fix takes agent-to-agent with it. The tools reach mod/messaging by
direct calls under the bearer's identity, not through `astral-query`, so the
origin guard never stands between them.
