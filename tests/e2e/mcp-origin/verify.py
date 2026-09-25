#!/usr/bin/env python3
"""Oracle: the door is shut on operations and open to agents.

Five judgements, and the order matters. The authority's record comes first,
because the refusals and the exchange below prove nothing about the guard or
about reach unless the authority admitted the questions that produced them.
The record also shows which question guards what: mod.mcp.call_agent_action is
asked of astral-query alone, and mail asks mod.messaging.send_action and
mod.messaging.receive_action. Then the controls: if alpha, holding the permits,
cannot reach mcp.list_agents and messaging.create_identity over apphost either,
the refusals below say nothing about the guard and this test has proven
nothing. Only once the ops are known to answer alpha does alpha's refusal over
MCP mean the guard refused it.

The refusals are then checked for being refusals. A timeout, a dead listener or
a transport error all leave the caller with no result, and a test that accepts
any failure would stay green if the MCP server stopped answering entirely.

Last, the exchange: a guard that also closed agent-to-agent would satisfy every
check above and destroy the product.
"""
from lib.authority import CALL, RECEIVE, SEND, answers
from lib.sessionio import load

# what a refusal reads like at the tool boundary: mod/shell answers ErrRejected,
# and mod/mcp's tool wraps the routing error in this text.
REFUSAL_MARKERS = ("query failed", "rejected", "access denied")

# what a refusal must NOT read like — these mean the call never got a verdict.
# "unknown target" is the call gate's refusal, which stops a query before it
# reaches the guard under test.
#
# why route-not-found is listed, and listed first: mod/mcp wraps every routing
# error as "query failed: %v" (mod/mcp/src/tool_query.go:86), and "query failed"
# is a refusal marker above. An op that is unmounted, renamed or simply absent
# therefore reads as a refusal on the marker alone — so an unmounted shell.shell
# with the origin guard removed would keep this test green. Checked after the
# markers, this is what separates "the guard refused it" from "nothing answered".
NOT_A_REFUSAL = ("route not found", "route_not_found", "routenotfound",
                 "HTTP 401", "HTTP 403", "HTTP 404", "HTTP 500",
                 "unknown target", "undecodable")


def asked(facts, kind, actor, other):
    """The answers the authority gave to one (action, actor, other party)."""
    return answers(facts["questions"], kind, actor, other)


def check_authority(facts):
    """The authority was asked, naming the right actor, before every call and
    every message the checks below rely on."""
    node, a, b, g = facts["node"], facts["alpha"], facts["beta"], facts["gamma"]

    for kind, actor, other, who in (
            (CALL, a, node, "alpha calling the node"),
            (SEND, a, b, "alpha sending to beta"),
            (RECEIVE, b, a, "beta receiving from alpha"),
            (SEND, b, a, "beta sending to alpha"),
            (RECEIVE, a, b, "alpha receiving from beta")):
        got = asked(facts, kind, actor, other)
        assert got and all(got), (
            f"the authority answered {got!r} for {who} — node1 did not put "
            "the question to the configured authority, or put it naming the "
            f"wrong actor; it was asked {facts['questions']!r}")

    assert asked(facts, SEND, a, g) == [False], (
        f"the authority answered {asked(facts, SEND, a, g)!r} for alpha sending "
        "to gamma, not one refusal — the refusal of gamma below is not the "
        "authority's")

    # mod.mcp.call_agent_action guards astral-query and declared tools, never
    # mail: the only calls asked are alpha's queries to the node.
    calls = {(q["actor"], q["other"]) for q in facts["questions"]
             if q["type"] == CALL}
    assert calls == {(a, node)}, (
        f"the authority was asked {CALL} for {calls!r}, not only for alpha "
        "calling the node — mail still asks the query action")


def main():
    doc = load()
    facts = doc["facts"]
    check_authority(facts)

    for name in ("read_beta", "read_gamma"):
        leaked = [k for k in facts[name] if "token" in k]
        assert not leaked, (
            f"mcp.agent answered {leaked} in {name} — the record a caller "
            "reads about an agent carries its credential")

    assert facts["control_agents"] >= 3, (
        f"mcp.list_agents named {facts['control_agents']} agents to alpha over "
        "apphost, not the three the driver minted — the refusals below are not "
        "evidence about the guard, because the op does not answer alpha at all")
    assert facts["control_identity"], (
        "messaging.create_identity minted nothing for alpha over apphost — its "
        "refusal below is not evidence about the guard, because the op does "
        "not answer alpha at all")

    assert "astral-query" in facts["tools"], (
        f"the agent's tool set is {facts['tools']}, without astral-query — "
        "nothing below exercised the path under test")

    for op, r in facts["refusals"].items():
        assert r["refused"], (
            f"{op} answered an agent: {r['detail']} — a node op admitted a "
            "query carrying MCP origin")

        detail = r["detail"].lower()
        assert not any(m in detail for m in (s.lower() for s in NOT_A_REFUSAL)), (
            f"{op} failed without being refused: {r['detail']} — the call did "
            "not reach the guard, so its refusal proves nothing")
        assert any(m in detail for m in REFUSAL_MARKERS), (
            f"{op} failed with {r['detail']}, which names no refusal — a "
            "refusal the caller cannot read is indistinguishable from a fault")

    u = facts["unadmitted"]
    assert u["refused"], (
        f"gamma answered: {u['detail']} — an agent is reachable without its "
        "authority admitting the call")
    assert "unknown recipient" in u["detail"].lower(), (
        f"the send to gamma failed with {u['detail']} — an agent the authority "
        "refuses reads as an identity the node never heard of")

    x = facts["exchange"]
    assert not x["beta_timed_out"] and x["beta_waited"], (
        f"beta's wait timed out with ids={x['beta_waited']!r} — the guard "
        "closed agent-to-agent along with the operations")
    assert x["beta_heard"] == facts["ask"], (
        f"beta heard {x['beta_heard']!r}, not {facts['ask']!r}")
    assert x["beta_sender"] == x["alpha_sender_expected"], (
        f"beta's sender was {x['beta_sender']}, not alpha "
        f"({x['alpha_sender_expected']}) — the message carried the wrong identity")
    assert x["alpha_got"] == facts["answer"], (
        f"alpha got {x['alpha_got']!r}, not {facts['answer']!r} — the reply "
        "never reached alpha's inbox")
    assert x["reply_parent"] == x["alpha_asked"], (
        f"the reply named {x['reply_parent']!r} as the message it answers, "
        f"not {x['alpha_asked']!r} — a reply that names nothing is one alpha "
        "has to match by sender and recency")

    ops = ", ".join(facts["refusals"])
    print(f"oracle: alpha was refused {ops} over MCP while the same op answers "
          f"alpha over apphost, the authority kept gamma out, and alpha and "
          f"beta exchanged {len(facts['ask'])} B both ways")


main()
