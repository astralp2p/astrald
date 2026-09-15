#!/usr/bin/env python3
"""Driver: an agent reaches its peers through the MCP door and no node ops.

astral-query hands the model a target and a path and routes whatever it names.
Every astrald operation is mounted behind one router — mod/shell mounts each
module's op router as a scope and answers on the node's own identity — so an
agent that may query the node may call all of them. mod/mcp marks its queries
with astral.OriginMCP and mod/shell refuses that origin.

Reach between agents is not the node's to give. mod/mcp asks
mod.mcp.call_agent_action of the calling agent and mod.mcp.answer_agent_action
of the called one, and the harness points both at an external authority on the
node's own loopback port. This driver serves that authority: it admits alpha
and beta to each other and nothing else.

The driver acts and judges nothing: it mints three agents over apphost, grants
alpha the permits the node ops ask, tries those ops as alpha over MCP, records
what one of them answers alpha over apphost, sends to an agent the authority
does not admit, runs one exchange between the two agents it does, and records
every question the authority was asked.

why the control call: "refused" and "does not exist" leave a caller holding the
same nothing. Recording mcp.list_agents answered to alpha over apphost, in this
same run and on this same node, is what makes the refusals evidence about the
guard rather than about the op or alpha's permits.

why the exchange is sequential: a message is stored in the recipient's inbox by
the recipient's node (mod/mcp RouteQuery), so beta collects it on its next
wait, and reads the body with read_messages. Neither agent has to be
running while the other writes.
"""
import asyncio
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

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

# why alpha is granted what the node ops ask: each op refuses a caller without
# its permit, and a refusal that the permit alone explains says nothing about
# the origin. Holding both, alpha is refused over MCP by the origin guard only.
GRANTS = ["mod.shell.shell_action", "mod.auth.admin_manage_apps_action"]

ASK = "alpha-asks-0xC0FFEE"
ANSWER = "beta-answers-0xBEEF"

CALL = "mod.mcp.call_agent_action"
ANSWER_ACTION = "mod.mcp.answer_agent_action"


class Authority:
    """The external authority node1 asks about calls between agents.

    It allows a question only when (action type, actor, other party) is in
    `allowed`, and records every question with the answer it gave. The other
    party is ToID for a call and FromID for an answer.
    """

    def __init__(self, url: str, allowed: set):
        self.allowed = allowed
        self.questions = []
        authority = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                allow = authority.decide(json.loads(body))
                answer = json.dumps({"allow": allow}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(answer)))
                self.end_headers()
                self.wfile.write(answer)

            def log_message(self, *args):
                pass

        where = urlsplit(url)
        self.server = ThreadingHTTPServer((where.hostname, where.port), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def decide(self, envelope: dict) -> bool:
        kind = envelope.get("Type")
        obj = envelope.get("Object") or {}
        actor = ((obj.get("Action") or {}).get("ActorID") or "").lower()
        other = (obj.get("ToID") or obj.get("FromID") or "").lower()
        allow = (kind, actor, other) in self.allowed
        self.questions.append(
            {"type": kind, "actor": actor, "other": other, "allow": allow})
        return allow

    def close(self):
        self.server.shutdown()
        self.server.server_close()


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
        gamma = await mint(c, "gamma")

        for action in GRANTS:
            await c.call_raw(f"apphost.grant?identity={alpha['identity']}"
                             f"&action={action}&out=json")

        # mcp.agent is the op the dashboard reads per agent, and the record it
        # answers must not carry the agent's token.
        read_beta = _docs(await c.call_raw(
            f"mcp.agent?identity={beta['identity']}&out=json"))[0]
        read_gamma = _docs(await c.call_raw(
            f"mcp.agent?identity={gamma['identity']}&out=json"))[0]

    # the control is alpha itself over apphost: the same identity asking the
    # same op of the same node, with only the origin differing from the refusal
    async with await astral.connect(n1["endpoint"], token=alpha["token"]) as c:
        control = _docs(await c.call_raw("mcp.list_agents?out=json"))

    a, b, g = (x["identity"].lower() for x in (alpha, beta, gamma))
    node = n1["identity"].lower()
    facts = {
        "control_agents": len(control),
        "read_beta": read_beta,
        "read_gamma": read_gamma,
        "node": node,
        "alpha": a,
        "beta": b,
        "gamma": g,
        "ask": ASK,
        "answer": ANSWER,
        "refusals": {},
        "exchange": {},
    }

    # alpha and beta may call and answer each other; gamma is admitted to
    # nothing, which is what an agent is until an authority says otherwise.
    #
    # why alpha may call the node: astral-query asks call_agent_action before
    # it routes anything, so an agent the authority keeps from the node never
    # reaches mod/shell, and the origin refusal would go unexercised.
    authority = Authority(n1["authority_url"], {
        (CALL, a, node),
        (CALL, a, b), (ANSWER_ACTION, b, a),
        (CALL, b, a), (ANSWER_ACTION, a, b),
    })
    try:
        await exercise(n1, alpha, beta, gamma, facts)
    finally:
        authority.close()
    facts["questions"] = authority.questions
    write_facts(facts)

    refused = sum(1 for r in facts["refusals"].values() if r["refused"])
    x = facts["exchange"]
    print(f"driver: {refused}/{len(NODE_OPS)} node ops refused to an agent; "
          f"unadmitted agent refused={facts['unadmitted']['refused']}; "
          f"authority asked {len(authority.questions)} questions; "
          f"beta heard {x['beta_heard']!r}, alpha got {x['alpha_got']!r}")


async def exercise(n1, alpha, beta, gamma, facts):
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

        # an agent the authority does not admit is unreachable, and unreachable
        # the way an identity the node never heard of is
        try:
            out = agent_alpha.call_tool("send_message", {
                "to": gamma["identity"], "content": ASK,
            })
            facts["unadmitted"] = {"refused": False,
                                   "detail": json.dumps(out)[:400]}
        except ToolError as e:
            facts["unadmitted"] = {"refused": True, "detail": str(e)[:400]}

        asked = _structured(agent_alpha.call_tool("send_message", {
            "to": beta["identity"], "content": ASK,
        }))

        # beta waits, then reads: the park answers what arrived without its
        # body, and reading is the separate act that hands one out.
        with MCPClient(n1["mcp_url"], beta["token"]) as agent_beta:
            waited = _structured(agent_beta.call_tool(
                "wait", {"timeout_secs": 10}))
            envelope = (waited.get("messages") or [{}])[0]
            heard = _structured(agent_beta.call_tool("read_messages", {
                "ids": [{"box": envelope.get("box"), "id": envelope.get("id")}],
            }))
            beta_msg = (heard.get("messages") or [{}])[0]

            # the reply names the message it answers
            agent_beta.call_tool("send_message", {
                "to": beta_msg.get("sender", alpha["identity"]),
                "content": ANSWER,
                "parent_id": beta_msg.get("id"),
            })

        back = _structured(agent_alpha.call_tool("wait", {"timeout_secs": 10}))
        reply_env = (back.get("messages") or [{}])[0]
        got = _structured(agent_alpha.call_tool("read_messages", {
            "ids": [{"box": reply_env.get("box"), "id": reply_env.get("id")}],
        }))
        alpha_msg = (got.get("messages") or [{}])[0]

    facts["exchange"] = {
        "beta_timed_out": waited.get("timed_out"),
        "beta_waited": [m.get("id") for m in (waited.get("messages") or [])],
        "beta_heard": beta_msg.get("content"),
        "beta_sender": beta_msg.get("sender"),
        "alpha_sender_expected": alpha["identity"],
        "alpha_got": alpha_msg.get("content"),
        "alpha_asked": asked.get("id"),
        "reply_parent": alpha_msg.get("parent_id"),
    }


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
