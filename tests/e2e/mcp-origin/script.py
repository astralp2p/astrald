#!/usr/bin/env python3
"""Driver: an agent reaches its peers through the MCP door and no node ops.

astral-query hands the model a target and a path and routes whatever it names.
Every astrald operation is mounted behind one router — mod/shell mounts each
module's op router as a scope and answers on the node's own identity — so an
agent that may query the node may call all of them. mod/mcp marks its queries
with astral.OriginMCP and mod/shell refuses that origin.

The driver does four things and judges none of them: it mints two agents over
apphost, tries three node operations as an agent, records what the same
operation does for a non-agent caller, and runs one agent-to-agent exchange.
The oracle decides what those four outcomes mean together.

why the control call: "refused" and "does not exist" leave a caller holding the
same nothing. Recording mcp.list_agents over apphost, in this same run and on
this same node, is what makes the refusals evidence about the guard rather than
about the op.

why the exchange is sequential: a query for an agent that is not listening is
accepted and queued (mod/mcp RouteQuery), so beta collects it on its next
astral-listen. No thread has to hold a listener open.
"""
import asyncio
import json

import astral

from lib.mcpclient import MCPClient, ToolError
from lib.sessionio import load, write_facts

# Every op here answers an agent when the guard is removed — that is the bar
# for membership. shell.shell is an interactive op shell over the whole scope
# tree; mcp.list_agents is mod/mcp's own, and returns every tenant's agent.
#
# why not nodes.new_link: it refuses an argument-less call on its own, so it
# reads the same guarded or not and witnesses nothing.
NODE_OPS = ["shell.shell", "mcp.list_agents"]

ASK = "alpha-asks-0xC0FFEE"
ANSWER = "beta-answers-0xBEEF"


def _docs(raw: bytes) -> list:
    """The objects an op streamed, unwrapped.

    An op answers one JSON document per line, each a {Type, Object} envelope.
    Keys are lowered so a driver reads `token`, not `Token`.
    """
    out = []
    for line in raw.decode().splitlines():
        if not line.strip():
            continue
        doc = json.loads(line)
        obj = doc.get("Object", doc)
        # the stream ends with an eos envelope carrying a null object
        if isinstance(obj, dict):
            out.append({k.lower(): v for k, v in obj.items()})
    return out


async def mint(client, alias: str) -> dict:
    raw = await client.call_raw(f"mcp.create_agent?alias={alias}&out=json")
    return _docs(raw)[0]


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        alpha = await mint(c, "alpha")
        beta = await mint(c, "beta")
        control = _docs(await c.call_raw("mcp.list_agents?out=json"))

    facts = {
        "control_agents": len(control),
        "ask": ASK,
        "answer": ANSWER,
        "refusals": {},
        "exchange": {},
    }

    with MCPClient(n1["mcp_url"], alpha["token"]) as agent_alpha:
        facts["tools"] = sorted(t["name"] for t in agent_alpha.list_tools())

        for op in NODE_OPS:
            try:
                out = agent_alpha.call_tool(
                    "astral-query",
                    {"target": n1["identity"], "path": op, "timeout_ms": 5000})
                # answered: record it, so the oracle reports an open guard
                # rather than a missing assertion
                facts["refusals"][op] = {"refused": False,
                                         "detail": json.dumps(out)[:400]}
            except ToolError as e:
                facts["refusals"][op] = {"refused": True, "detail": str(e)[:400]}

        opened = agent_alpha.call_tool("astral-query", {
            "target": beta["identity"], "path": "chat",
            "payload": ASK, "session": True, "timeout_ms": 10000,
        })
        alpha_session = _structured(opened)["session_id"]

        with MCPClient(n1["mcp_url"], beta["token"]) as agent_beta:
            heard = _structured(agent_beta.call_tool(
                "astral-listen", {"timeout_ms": 10000}))
            agent_beta.call_tool("astral-send", {
                "session_id": heard["session_id"],
                "data": ANSWER, "close": True,
            })

        got = _structured(agent_alpha.call_tool(
            "astral-receive",
            {"session_id": alpha_session, "timeout_ms": 10000}))

    facts["exchange"] = {
        "beta_status": heard.get("status"),
        "beta_heard": heard.get("payload"),
        "beta_caller": heard.get("caller"),
        "alpha_caller_expected": alpha["identity"],
        "alpha_got": got.get("payload"),
    }
    write_facts(facts)

    refused = sum(1 for r in facts["refusals"].values() if r["refused"])
    print(f"driver: {refused}/{len(NODE_OPS)} node ops refused to an agent; "
          f"beta heard {heard.get('payload')!r}, alpha got {got.get('payload')!r}")


def _structured(result: dict) -> dict:
    """The tool's structured output, whichever field the SDK put it in."""
    if "structuredContent" in result:
        return result["structuredContent"]
    for c in result.get("content", []):
        if isinstance(c, dict) and c.get("type") == "text":
            try:
                return json.loads(c["text"])
            except (ValueError, KeyError):
                continue
    return {}


asyncio.run(main())
