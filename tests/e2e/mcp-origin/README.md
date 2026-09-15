# mcp-origin — an agent reaches its peers and no node operations

`astral-query` takes a target and a path from the model and routes what it
names. Every astrald operation sits behind one router: mod/shell mounts each
loaded module's op router as a scope and answers on the node's own identity, so
an agent permitted to query the node can call all 125 of them.

mod/mcp marks the queries it routes with `astral.OriginMCP`, and mod/shell
refuses that origin before reaching the scopes.

## What this proves that the unit tests do not

The unit tests hold each half — that `launch` stamps, that mod/shell refuses a
stamped query. Neither runs the path between them: a real bearer token, the real
streamable-HTTP listener, `core.Router`, and whichever router claims the target.
This drives that path.

## Why the driver serves an authority

mod/mcp holds no reachability of its own. A call between agents proceeds only
when `mod.mcp.call_agent_action` is granted to the caller and
`mod.mcp.answer_agent_action` to the agent called, and no handler grants either,
so a node answers no call unless an external authority does. The harness names
one for every local node in `auth.yaml`, on the node's own loopback port
(`authority_url` in `session.json`); an unserved port refuses.

The driver serves it. It admits alpha to the node, alpha and beta to each other
in both directions, and nothing else, and it records every question with its
answer. The oracle checks that record first: the refusal of gamma is evidence
only if the authority was asked and refused it, and the exchange is evidence
only if the authority was asked, naming the right actor, and admitted it.

`astral-query` asks `mod.mcp.call_agent_action` before it routes anything. An
agent the authority keeps from the node is refused there with `unknown target`
and never reaches mod/shell, so alpha is admitted to the node, and `unknown
target` does not count as the refusal under test.

## Why the control call matters

A refusal and a missing operation leave the caller holding the same nothing.
Each node op also refuses a caller without its permit, so alpha is granted
`mod.shell.shell_action` and `mod.auth.admin_manage_apps_action`, and the driver
records `mcp.list_agents` answered to alpha over apphost, on the same node in
the same run. The oracle checks it first. Only the origin differs between that
answer and the refusal over MCP.

The oracle also requires each failure to *read* as a refusal. A timeout, a 401
or a dead listener would otherwise keep this green while the guard was gone.

## Why an exchange is in here

A guard that refused everything would satisfy every check above and destroy the
product. alpha queries beta and beta answers, so the test fails if the fix takes
agent-to-agent with it.
