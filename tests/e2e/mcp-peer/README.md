# mcp-peer — a peer node reads an agent's declared tool as the agent's

An agent on node1 calls declared tools that name node2, and node2 reads the
agent as the caller: its app hears the agent, and its operations refuse an
agent that holds no permit while they answer node1.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — point the alias `peer` at node2, mint an agent on
  node1 with `mcp.create_agent`, push its relay contract to node2, record node1's
  own answers from node2's `nodes.links` and `user.info`, serve an app on node2
  that answers its caller, and call every tool of `lib/nodeconfig.py`
  `PEER_TOOLS` as the agent over MCP.
- **oracle** `verify.py` — node2 answered node1 both ops and took the relay
  contract; node2's app heard the agent; node2 refused both ops to the agent,
  and `user.info` refused it with code 4.

## What this proves that the unit tests do not

The unit test `TestADeclaredToolRoutesAsTheNode` holds that a declared tool
routes on the node's context with the agent as the caller. mod/nodes carries
such a query over a link as a relay query naming the agent, and one routed on a
context naming the agent as a plain query the far node answers as node1's
(`mod/nodes/src/mux.go`). This drives the whole path: a tool read from
`mcp.yaml`, `core.Router`, a real link, and node2's routing.

## Why the controls matter

node1 is a sibling in node2's swarm, so node2 answers node1 `nodes.links` and
`user.info`. A tool whose query crossed the link as node1's would be answered
both. The driver asks both as node1 over apphost in this run, so a refusal to
the agent is evidence about the caller node2 read.

## Why the relay contract is pushed

node2 admits a relay query naming the agent only under the relay contract the
agent issued to node1. `mcp.create_agent` signs one, and only node1 holds it.
Without the push every peer tool is refused at the link, and the refusals
would say nothing about the operations.

## Why an app is in here

A guard that refused every query leaving over a link would satisfy the
refusals. The app on node2 answers the caller it reads, so the test fails if
the agent reaches no peer at all, and fails if node2 reads node1 as the caller.
`user.info` refuses a caller outside the swarm with code 4, and a relay
refusal carries code 1, so the code places that refusal at the op.
