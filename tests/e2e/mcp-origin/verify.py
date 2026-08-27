#!/usr/bin/env python3
"""Oracle: the door is shut on operations and open to agents.

Five judgements, and the order matters. The visibility flags come first, because
the exchange below proves nothing about reach if nothing recorded the opt-in
that admitted it — read back through mcp.agent, not the acks. Then the control:
if a non-agent
caller cannot reach mcp.list_agents either, then the refusals below say nothing
about the guard and this test has proven nothing. Only once the op is known
reachable does an agent's refusal mean the guard refused it.

The refusals are then checked for being refusals. A timeout, a dead listener or
a transport error all leave the caller with no result, and a test that accepts
any failure would stay green if the MCP server stopped answering entirely.

Last, the exchange: a guard that also closed agent-to-agent would satisfy every
check above and destroy the product.
"""
from lib.sessionio import load

# what a refusal reads like at the tool boundary: mod/shell answers ErrRejected,
# and mod/mcp's tool wraps the routing error in this text.
REFUSAL_MARKERS = ("query failed", "rejected", "access denied")

# what a refusal must NOT read like — these mean the call never got a verdict.
NOT_A_REFUSAL = ("HTTP 401", "HTTP 403", "HTTP 404", "HTTP 500",
                 "unknown target", "undecodable")


def main():
    doc = load()
    facts = doc["facts"]

    beta, gamma = facts["read_beta"], facts["read_gamma"]
    delta = facts["read_delta"]

    assert beta.get("visible") is True, (
        f"beta reads visible={beta.get('visible')!r} after mcp.set_visible — "
        "the write did not land, so the exchange below proves nothing")
    assert gamma.get("visible") is False, (
        f"gamma reads visible={gamma.get('visible')!r} without anyone opening "
        "it — a new agent is reachable by default")

    assert delta.get("visible") is True, (
        f"delta reads visible={delta.get('visible')!r} after create_agent was "
        "asked for an open agent — the argument did not reach the row, and the "
        "caller holds an agent it believes is reachable")

    for name, rec in (("beta", beta), ("gamma", gamma), ("delta", delta)):
        leaked = [k for k in rec if "token" in k]
        assert not leaked, (
            f"mcp.agent answered {leaked} for {name} — the record a caller "
            "reads about an agent carries its credential")

    assert facts["control_agents"] >= 4, (
        f"mcp.list_agents named {facts['control_agents']} agents over apphost, "
        "not the four the driver minted — the refusals below are not evidence "
        "about the guard, because the op is unreachable for everyone")

    assert "astral-query" in facts["tools"], (
        f"the agent's tool set is {facts['tools']}, without astral-query — "
        "nothing below exercised the path under test")

    for op, r in facts["refusals"].items():
        assert r["refused"], (
            f"{op} answered an agent: {r['detail']} — mod/shell admitted a "
            "query carrying MCP origin")

        detail = r["detail"].lower()
        assert not any(m in detail for m in (s.lower() for s in NOT_A_REFUSAL)), (
            f"{op} failed without being refused: {r['detail']} — the call did "
            "not reach the guard, so its refusal proves nothing")
        assert any(m in detail for m in REFUSAL_MARKERS), (
            f"{op} failed with {r['detail']}, which names no refusal — a "
            "refusal the caller cannot read is indistinguishable from a fault")

    u = facts["invisible"]
    assert u["refused"], (
        f"a closed agent answered: {u['detail']} — an agent is reachable "
        "before its account holder opts in")
    assert not any(m in u["detail"].lower()
                   for m in (s.lower() for s in NOT_A_REFUSAL)), (
        f"the closed agent failed without being refused: {u['detail']}")

    x = facts["exchange"]
    assert x["beta_status"] == "message", (
        f"beta's read_next returned {x['beta_status']!r}, not a message — the "
        "guard closed agent-to-agent along with the operations")
    assert x["beta_heard"] == facts["ask"], (
        f"beta heard {x['beta_heard']!r}, not {facts['ask']!r}")
    assert x["beta_sender"] == x["alpha_sender_expected"], (
        f"beta's sender was {x['beta_sender']}, not alpha "
        f"({x['alpha_sender_expected']}) — the message carried the wrong identity")
    assert x["alpha_got"] == facts["answer"], (
        f"alpha got {x['alpha_got']!r}, not {facts['answer']!r} — the reply "
        "never reached alpha's inbox")

    ops = ", ".join(facts["refusals"])
    print(f"oracle: an agent was refused {ops} and a closed agent, while the "
          f"same node answers mcp.list_agents to apphost and alpha and beta "
          f"exchanged {len(facts['ask'])} B both ways")


main()
