#!/usr/bin/env python3
"""Driver: an agent on node1 calls declared tools that name node2.

node1 declares the tools lib/nodeconfig.PEER_TOOLS lists, each put to the
alias PEER_ALIAS. The driver points that alias at node2, mints one agent on
node1 with mcp.create_agent, pushes the agent's relay contract to node2, and
serves an app on node2 that answers the caller it reads. It then calls every
peer tool as the agent over MCP.

why the controls: node1 asks node2 nodes.links and user.info as itself, over
apphost, in this run. Both answer node1, so a refusal of the agent's tool is
evidence about the caller node2 read, and not about the op or the link.

why the push: node2 admits a relay query naming the agent only under the relay
contract the agent issued to node1 (mod/nodes/src/mux.go handleRelayQuery).
messaging.create_identity signs one, but only node1 holds it; node2 indexes a
relay contract pushed by the sibling it names as subject
(mod/user/src/object_receiver.go).

The driver acts and judges nothing.
"""
import asyncio
import json

import astral

from lib import jsonops, mail
from lib.mcpclient import MCPClient, ToolError
from lib.nodeconfig import PEER_ALIAS, PEER_TOOLS
from lib.sessionio import load, write_facts

APP_OP = "test.mcp_peer.caller"
AGENT_ALIAS = "peer-alpha"
RELAY = "mod.nodes.relay_for_action"


def _docs(raw: bytes) -> list:
    """The objects an op streamed, unwrapped, with keys lowered."""
    out = []
    for line in raw.decode().splitlines():
        if not line.strip():
            continue
        obj = json.loads(line).get("Object")
        if isinstance(obj, dict):
            out.append({k.lower(): v for k, v in obj.items()})
    return out


async def prepare(n1: dict, n2: dict) -> dict:
    """The alias, the agent, its relay contract pushed to node2, and node1's
    own answers from node2."""
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.call_raw(f"dir.set_alias?identity={n2['identity']}"
                         f"&alias={PEER_ALIAS}&out=json")
        agent = _docs(await c.call_raw(
            f"mcp.create_agent?alias={AGENT_ALIAS}&out=json"))[0]
        relay = [x for x in await mail.contracts_of(c, agent["identity"])
                 if mail.permits(x) == [(RELAY, 0, None)]]
        pushed = await jsonops.call(c, "objects.push?in=json&out=json",
                                    relay[0], eos=True, target=n2["identity"])
        links = await jsonops.stream(c, "nodes.links?out=json",
                                     target=n2["identity"])
        info = await jsonops.value(c, "user.info?out=json",
                                   target=n2["identity"])
    return {"agent": agent["identity"].lower(), "token": agent["token"],
            "pushed": pushed, "control_links": len(links),
            "control_info": bool(info.get("ContractID"))}


def call_tools(n1: dict, token: str) -> dict:
    """Every peer tool, called as the agent: what it answered or refused."""
    calls = {}
    with MCPClient(n1["mcp_url"], token) as agent:
        tools = sorted(t["name"] for t in agent.list_tools())
        for path, tool in PEER_TOOLS.items():
            try:
                out = agent.call_tool(tool, {})
                calls[path] = {"answered": True, "result": out}
            except ToolError as e:
                calls[path] = {"answered": False, "detail": str(e)[:400]}
    return {"tools": tools, "calls": calls}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = {"node1": n1["identity"].lower(), "node2": n2["identity"].lower()}
    facts.update(await prepare(n1, n2))

    # why node2's own token: the app registers under the identity its serving
    # connection authenticates as, and the peer tool names node2.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as host:
        heard = []

        async def answer_caller(q):
            heard.append(str(q.caller).lower())
            async with await q.accept() as stream:
                await stream.send(q.caller)
                await stream.send_eos()

        async with await host.serve() as svc:
            svc.mount(APP_OP, answer_caller)
            facts.update(await asyncio.to_thread(call_tools, n1, facts["token"]))
    facts["app_heard"] = heard
    del facts["token"]
    write_facts(facts)

    print("driver: " + ", ".join(
        f"{path} {'answered' if c['answered'] else 'refused'}"
        for path, c in facts["calls"].items()) + f"; the app heard {heard}")


asyncio.run(main())
