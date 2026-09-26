#!/usr/bin/env python3
"""Oracle: a peer node reads an agent's declared tool as the agent's.

The controls come first. node1 asked node2 nodes.links and user.info as
itself and both answered; without that, a refusal below says nothing about
which caller node2 read. The relay contract push comes next: node2 admits a
relay query naming the agent only under that contract.

Then the app: node2's app heard the agent, and the tool answered it the
agent's identity. A query crossing the link as node1's would name node1.

Last, the operations: node2 refused both to the agent, and each failure reads
as a refusal. user.info refuses a caller outside the swarm with code 4, which
a relay refusal never carries, so its code places the refusal at the op.
"""
import json

from lib.nodeconfig import PEER_TOOLS
from lib.sessionio import load

# what a refusal reads like at the tool boundary: mod/mcp wraps a routing
# error as "query failed: %v", and a rejection reads "query rejected (<code>)"
REFUSAL_MARKER = "query rejected"

# what a refusal must not read like: each means the call never got a verdict
NOT_A_REFUSAL = ("route not found", "unknown target", "HTTP 401", "HTTP 404",
                 "HTTP 500", "undecodable")


def check_controls(facts):
    assert facts["control_links"] >= 1, (
        "node2 answered node1 no link over nodes.links — the refusal below is "
        "not evidence about the agent, because the op does not answer node1")
    assert facts["control_info"], (
        "node2 answered node1 no contract over user.info — the refusal below is "
        "not evidence about the agent, because the op does not answer node1")
    assert facts["pushed"][:1] == [{"Type": "bool", "Object": True}], (
        f"node2 answered {facts['pushed']!r} to the agent's relay contract — "
        "node2 cannot admit a relay query naming the agent")


def check_tools(facts):
    missing = [t for t in PEER_TOOLS.values() if t not in facts["tools"]]
    assert not missing, (
        f"the agent is not served {missing} — node1's mcp.yaml did not declare "
        "the peer tools")


def check_app(facts):
    app = facts["calls"]["test.mcp_peer.caller"]
    assert app["answered"], (
        f"the peer app tool failed: {app.get('detail')} — the query never "
        "reached node2's app")
    assert facts["app_heard"] == [facts["agent"]], (
        f"node2's app read the caller as {facts['app_heard']!r}, not the agent "
        f"{facts['agent']} — the query crossed the link as "
        f"{'node1' if facts['node1'] in facts['app_heard'] else 'someone else'}")
    answer = json.dumps(app["result"]).lower()
    assert facts["agent"] in answer, (
        f"the tool answered {answer[:300]} — it does not carry the caller the "
        "app heard")


def check_ops(facts):
    for path in ("nodes.links", "user.info"):
        c = facts["calls"][path]
        assert not c["answered"], (
            f"{path} on node2 answered node1's agent: "
            f"{json.dumps(c.get('result'))[:300]} — the agent reached node2 "
            "with node1's authority")
        detail = c["detail"].lower()
        assert not any(m.lower() in detail for m in NOT_A_REFUSAL), (
            f"{path} failed without being refused: {c['detail']}")
        assert REFUSAL_MARKER in detail, (
            f"{path} failed with {c['detail']}, which names no refusal")

    info = facts["calls"]["user.info"]["detail"]
    assert "(4)" in info, (
        f"user.info refused the agent with {info!r}, not code 4 — the refusal "
        "did not come from the op reading a caller outside the swarm")


def main():
    facts = load()["facts"]
    check_controls(facts)
    check_tools(facts)
    check_app(facts)
    check_ops(facts)
    print(f"oracle: node2 answered node1 nodes.links and user.info, read the "
          f"agent {facts['agent'][:16]}… as the caller of its app, and refused "
          "both ops to the agent")


main()
