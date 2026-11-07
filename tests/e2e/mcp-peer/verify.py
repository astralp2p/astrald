#!/usr/bin/env python3
"""Oracle: a peer node reads an agent's declared tool as the agent's.

The controls come first. node1 asked node2 nodes.links and user.info as
itself and both answered; without that, a refusal below says nothing about
which caller node2 read. The relay contract push comes next: node2 admits a
relay query naming the agent only under that contract.

Then the app: node2's app heard the agent, and the tool answered it the
agent's identity. A query crossing the link as node1's would name node1.

Last, the operations: node2 refused both to the agent, and each refusal is the
op's. An op refuses a caller it does not authorize by accepting the query and
answering `not permitted`, while a relay refusal rejects the query before any
op runs, so that answer places the refusal at the op.
"""
import json

from lib.nodeconfig import PEER_TOOLS
from lib.sessionio import load

# what an op answers a caller it does not authorize, as the tool's objects:
# one error_message (astral-docs core-definitions/op.md), which mod/mcp hands
# the agent unread
NOT_PERMITTED = [{"Type": "error_message", "Object": "not permitted"}]


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
        assert c["answered"], (
            f"{path} failed for the agent with {c['detail']} — no op answered, "
            "so the failure says nothing about the caller node2 read")
        objects = c["result"].get("structuredContent", {}).get("objects")
        granted = [o for o in objects or [] if o.get("Type") != "error_message"]
        assert not granted, (
            f"{path} on node2 answered node1's agent: "
            f"{json.dumps(granted)[:300]} — the agent reached node2 "
            "with node1's authority")
        assert objects == NOT_PERMITTED, (
            f"{path} answered the agent {json.dumps(c['result'])[:300]}, which "
            "is not the op refusing its caller")


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
