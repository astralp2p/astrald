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

## Why the control call matters

A refusal and a missing operation leave the caller holding the same nothing. The
driver records `mcp.list_agents` answered over apphost, on the same node in the
same run, and the oracle checks it first. Without it, the test would pass
against a node whose operations were all broken.

The oracle also requires each failure to *read* as a refusal. A timeout, a 401
or a dead listener would otherwise keep this green while the guard was gone.

## Why an exchange is in here

A guard that refused everything would satisfy every check above and destroy the
product. alpha queries beta and beta answers, so the test fails if the fix takes
agent-to-agent with it.
