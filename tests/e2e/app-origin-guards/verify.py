#!/usr/bin/env python3
"""Oracle: the User reaches register, register_handler and swarm discovery on
node1, and a sibling relaying as that User does not.

Three judgements, and the order matters. The controls first: if the User cannot
reach these ops locally either, the refusals say nothing about the guards and
this test has proven nothing. Then the refusals, checked for being refusals — a
timeout, a lost route or an unreachable node all leave the caller with no
result, and a test accepting any failure would stay green if the link simply
broke. Each refusal must also read as the guard's own: the origin refusal for
the two apphost ops, the swarm-from-a-link refusal for discovery.

Last, what the node holds. Whether apphost.register was refused and whether an
identity was nevertheless provisioned are two claims, and the driver is the
wrong party for the second. The oracle lists node1's tokens itself: the count
the driver took before the probes must still hold, and the identity the control
registered must be among them.
"""
import asyncio

import astral

from lib.sessionio import load

# What a refusal must not read like — each means the call never got a verdict.
NOT_A_REFUSAL = ("timeout", "timed out", "route_not_found", "routenotfound",
                 "nodeunavailable", "unknown target", "connection refused")

# What the two apphost op pages name as the answer to a query off a link
# (astral-docs protocols/apphost/ops). The authorization check answers "not
# permitted" instead, so this text says the origin guard refused the probe, not
# a check the User passes anyway.
ORIGIN_REFUSAL = "origin not allowed: network"

# services.discover answers reach=swarm off a link with an error_message, not
# the origin refusal (mod/services/src/op_discover.go, errSwarmFromLink). Only
# the stable head of the sentence is matched, and not the bare `reach=swarm`:
# the client's error echoes the query string, which names it.
SWARM_REFUSAL = "reach=swarm is accepted only"

EXPECTED = {
    "apphost.register": ORIGIN_REFUSAL,
    "apphost.register_handler": ORIGIN_REFUSAL,
    "services.discover": SWARM_REFUSAL,
}


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]

    assert facts["control_registered"], (
        "apphost.register did not answer the User on node1 — the op is "
        "unreachable for everyone, so the refusal below is not evidence about "
        "the guard")
    assert facts["control_handler_acked"], (
        "apphost.register_handler did not acknowledge the User on node1 — the "
        "op is not working locally, so a refusal over the link proves nothing")
    cd = facts["control_discover"]
    assert cd["answered"] and cd["ended"], (
        f"services.discover reach=swarm did not run to its end for the User on "
        f"node1: {cd} — the op is not working locally, so a refusal over the "
        "link proves nothing")

    assert set(facts["refusals"]) == set(EXPECTED), (
        f"the driver probed {sorted(facts['refusals'])}, want {sorted(EXPECTED)}")
    for op, r in facts["refusals"].items():
        assert r["refused"], (
            f"{op} answered a sibling relaying as the User: {r['detail']} — "
            "the op is reachable from the network")

        detail = r["detail"].lower()
        assert not any(m in detail for m in NOT_A_REFUSAL), (
            f"{op} failed without being refused: {r['detail']} — the query did "
            "not reach the guard, so its failure proves nothing")
        want = EXPECTED[op]
        assert want in detail, (
            f"{op} failed with {r['detail']}, which is not the refusal "
            f"{want!r} — a refusal the caller cannot read is indistinguishable "
            "from a fault")

    # The oracle measures node1 rather than taking the driver's word for it.
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c1:
        tokens = await c1.apphost.list_tokens()
        for_control = await c1.apphost.list_tokens(facts["control_registered"])

    assert len(tokens) == facts["tokens_before"] == facts["tokens_after"], (
        f"node1 holds {len(tokens)} token(s); it held {facts['tokens_before']} "
        f"before the probes and {facts['tokens_after']} after — a refused "
        "apphost.register provisioned an identity anyway")
    assert len(for_control) == 1, (
        f"node1 holds {len(for_control)} token(s) for the identity the control "
        "registered, want 1")

    ops = ", ".join(facts["refusals"])
    print(f"oracle: node1 answers the User locally and refused {ops} to a "
          f"sibling relaying as that same User; its {len(tokens)} tokens did "
          "not change")


asyncio.run(main())
